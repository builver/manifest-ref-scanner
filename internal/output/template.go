package output

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/patri/manifest-ref-scanner/internal/registry"
)

// semverRe matches tags that are concrete semver versions (not ranges, aliases, or floating tags).
// Accepts v1, v1.2, v1.2.3, v1.2.3-rc.1, v1.2.3+build — same pattern as OCM enforces.
var semverRe = regexp.MustCompile(`^v?(0|[1-9]\d*)(\.(0|[1-9]\d*)(\.(0|[1-9]\d*))?)?(-[0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*)?(\+[0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*)?$`)

// Data is the root object available inside every output template.
type Data struct {
	Artifacts []*registry.Artifact
	Args      map[string]string
	// ScanPath is the directory that was scanned, as passed to the CLI.
	ScanPath string
}

// TemplateFormatter renders artifacts using a Go text/template.
// Built-in formats (ocm, bom) and custom format-config files both use this path.
type TemplateFormatter struct {
	tmpl     *template.Template
	args     map[string]string
	scanPath string
}

// NewTemplateFormatter constructs a TemplateFormatter from a FormatConfig.
// cfg.Args provides default values; overrides (from --arg flags) take precedence.
// scanPath is the directory that was scanned; available in templates as .ScanPath.
func NewTemplateFormatter(cfg FormatConfig, overrides map[string]string, scanPath string) (*TemplateFormatter, error) {
	src := cfg.Template
	if cfg.TemplateFile != "" {
		data, err := os.ReadFile(cfg.TemplateFile)
		if err != nil {
			return nil, fmt.Errorf("read template file: %w", err)
		}
		src = string(data)
	}
	if src == "" {
		return nil, fmt.Errorf("format config has no template or templateFile")
	}

	tmpl, err := template.New("output").Funcs(funcMap()).Parse(src)
	if err != nil {
		return nil, fmt.Errorf("parse output template: %w", err)
	}

	args := make(map[string]string, len(cfg.Args)+len(overrides))
	for k, v := range cfg.Args {
		args[k] = v
	}
	for k, v := range overrides {
		args[k] = v
	}

	return &TemplateFormatter{tmpl: tmpl, args: args, scanPath: scanPath}, nil
}

func (f *TemplateFormatter) Format(w io.Writer, artifacts []*registry.Artifact) error {
	var buf bytes.Buffer
	if err := f.tmpl.Execute(&buf, Data{Artifacts: artifacts, Args: f.args, ScanPath: f.scanPath}); err != nil {
		return err
	}
	_, err := buf.WriteTo(w)
	return err
}

// semverErrorMsg builds the human-readable error string for an artifact with no pinned version.
// Only called when the artifact has already been determined to have no usable pinned version.
func semverErrorMsg(a *registry.Artifact) string {
	hint := ""
	if sv, ok := a.Ref["semver"]; ok && sv != "" {
		hint = fmt.Sprintf("\n  semver range %q cannot be used as an OCM version — resolve it to a specific tag", sv)
	} else if a.Tag != "" {
		hint = fmt.Sprintf("\n  tag %q is not a valid semver version", a.Tag)
	}

	src := "unknown"
	if len(a.Resolution) > 0 {
		s := a.Resolution[0]
		src = fmt.Sprintf("%s %q", s.Kind, s.Name)
		if s.Namespace != "" {
			src += " in namespace " + s.Namespace
		}
		if s.File != "" {
			src += " (" + s.File + ")"
		}
	}

	return fmt.Sprintf(
		"artifact %q has no pinned version%s\n  source: %s\n  fix: replace the floating reference with a specific tag (e.g. v1.2.3) or a digest",
		a.Reference, hint, src,
	)
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		// uniqueByRaw deduplicates artifacts by their Raw reference, keeping the first occurrence.
		// Useful when the same image appears from multiple sources and you only want one output entry.
		"uniqueByRaw": func(artifacts []*registry.Artifact) []*registry.Artifact {
			seen := make(map[string]bool, len(artifacts))
			out := make([]*registry.Artifact, 0, len(artifacts))
			for _, a := range artifacts {
				if !seen[a.Reference] {
					seen[a.Reference] = true
					out = append(out, a)
				}
			}
			return out
		},

		// sanitizeName replaces characters invalid in OCM/Kubernetes names with hyphens.
		// Also strips semver/glob characters (*, >, <, =, ~, ^) that appear in range expressions.
		"sanitizeName": func(s string) string {
			r := strings.NewReplacer(
				"/", "-", ":", "-", "@", "-", "_", "-", ".", "-",
				"*", "", ">", "", "<", "", "=", "", "~", "", "^", "",
			)
			name := r.Replace(s)
			// collapse multiple consecutive hyphens and trim leading/trailing ones
			for strings.Contains(name, "--") {
				name = strings.ReplaceAll(name, "--", "-")
			}
			return strings.Trim(name, "-")
		},

		// trimPrefix removes a leading prefix from s, returning s unchanged if it does not start with prefix.
		"trimPrefix": func(prefix, s string) string {
			return strings.TrimPrefix(s, prefix)
		},

		// base returns the last element of a path, equivalent to filepath.Base.
		// Useful for deriving a component name from .ScanPath.
		"base": filepath.Base,

		// withVersion appends a version (tag or digest) to a reference if the reference
		// does not already carry one. Strips the "oci://" scheme. Handles digest (@sha256:...)
		// and tag (:v1.2.3) forms. Use in OCM imageReference fields when the scanner stores
		// the version separately from the URL (e.g. via SemverPaths).
		"withVersion": func(ref, ver string) string {
			ref = strings.TrimPrefix(ref, "oci://")
			if ver == "" {
				return ref
			}
			if strings.HasPrefix(ver, "sha256:") {
				if !strings.Contains(ref, "@") {
					return ref + "@" + ver
				}
				return ref
			}
			// tag: only append if the reference has no tag yet
			if !strings.Contains(ref, ":") {
				return ref + ":" + ver
			}
			return ref
		},

		// imageRef constructs a fully-qualified OCI reference from an artifact's parsed fields
		// (registry/repository) combined with the given resolved version (tag or digest).
		// Use this instead of .Reference in output formats like OCM that require a canonical
		// registry-qualified reference — short-form names like "nginx:..." lack the registry
		// domain and will fail OCM's URL resolver.
		"imageRef": func(a *registry.Artifact, ver string) string {
			base := ""
			if a.Registry != "" && a.Repository != "" {
				base = a.Registry + "/" + a.Repository
			} else if a.Repository != "" {
				base = a.Repository
			} else {
				// fall back to raw reference, strip scheme, strip any existing tag/digest
				base = strings.TrimPrefix(a.Reference, "oci://")
				base = strings.SplitN(base, ":", 2)[0]
				base = strings.SplitN(base, "@", 2)[0]
			}
			if ver == "" {
				return base
			}
			if strings.HasPrefix(ver, "sha256:") {
				return base + "@" + ver
			}
			return base + ":" + ver
		},

		// contains reports whether substr appears anywhere in s.
		"contains": func(s, substr string) bool {
			return strings.Contains(s, substr)
		},

		// hasPrefix reports whether s begins with prefix.
		"hasPrefix": func(s, prefix string) bool {
			return strings.HasPrefix(s, prefix)
		},

		// excludeRefs filters artifacts whose Reference contains any of the given comma-separated patterns.
		// Useful for dropping self-references or known intentional floating refs from output.
		// Example arg: --arg excludeRefs=ghcr.io/myorg/my-gitops-repo
		"excludeRefs": func(patterns string, artifacts []*registry.Artifact) []*registry.Artifact {
			if patterns == "" {
				return artifacts
			}
			parts := strings.Split(patterns, ",")
			out := make([]*registry.Artifact, 0, len(artifacts))
			for _, a := range artifacts {
				excluded := false
				for _, p := range parts {
					if p = strings.TrimSpace(p); p != "" && strings.Contains(a.Reference, p) {
						excluded = true
						break
					}
				}
				if !excluded {
					out = append(out, a)
				}
			}
			return out
		},

		// isSemver reports whether s is a concrete semver version (not a range, alias, or floating tag like "latest").
		// Use in templates to decide whether an artifact tag is suitable as an OCM/SBOM version.
		"isSemver": func(s string) bool {
			return s != "" && semverRe.MatchString(s)
		},

		// validateSemverAll checks every artifact in the list for a pinned version and
		// returns a combined error listing all violations. Call this at the top of templates
		// that require all artifacts to be pinned (e.g. OCM). Aborts template execution
		// before any output is written if any artifact fails.
		"validateSemverAll": func(artifacts []*registry.Artifact) (string, error) {
			seen := make(map[string]bool, len(artifacts))
			var errs []string
			for _, a := range artifacts {
				if seen[a.Reference] {
					continue
				}
				seen[a.Reference] = true
				if (a.Tag != "" && semverRe.MatchString(a.Tag)) || a.Digest != "" {
					continue
				}
				if sv := a.Ref["semver"]; sv != "" && semverRe.MatchString(sv) {
					continue
				}
				errs = append(errs, semverErrorMsg(a))
			}
			if len(errs) > 0 {
				return "", fmt.Errorf(
					"%d artifact(s) have no pinned version (OCM requires concrete semver or digest):\n\n%s",
					len(errs), strings.Join(errs, "\n\n"),
				)
			}
			return "", nil
		},

		// requireSemver returns the artifact's concrete version (tag or digest) for use in
		// formats like OCM that require pinned versions. Aborts template execution with a
		// detailed error when the artifact has no pinned version, telling the user exactly
		// which manifest field to fix.
		"requireSemver": func(a *registry.Artifact) (string, error) {
			if a.Tag != "" && semverRe.MatchString(a.Tag) {
				return a.Tag, nil
			}
			if sv := a.Ref["semver"]; sv != "" && semverRe.MatchString(sv) {
				return sv, nil
			}
			if a.Digest != "" {
				return a.Digest, nil
			}
			return "", fmt.Errorf("%s", semverErrorMsg(a))
		},

		// default returns val if non-empty, otherwise def.
		"default": func(def, val string) string {
			if val == "" {
				return def
			}
			return val
		},

		// required returns val if non-empty, otherwise a descriptive error.
		// Use in templates where an arg must be supplied by the caller.
		"required": func(name, val string) (string, error) {
			if val == "" {
				return "", fmt.Errorf("required arg %q is not set; pass --arg %s=<value>", name, name)
			}
			return val, nil
		},
	}
}
