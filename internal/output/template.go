package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	"github.com/patri/manifest-ref-scanner/internal/registry"
)

// Data is the root object available inside every output template.
type Data struct {
	Artifacts []*registry.Artifact
	Args      map[string]string
}

// TemplateFormatter renders artifacts using a Go text/template.
// Built-in formats (ocm, bom) and custom format-config files both use this path.
type TemplateFormatter struct {
	tmpl *template.Template
	args map[string]string
}

// NewTemplateFormatter constructs a TemplateFormatter from a FormatConfig.
// cfg.Args provides default values; overrides (from --arg flags) take precedence.
func NewTemplateFormatter(cfg FormatConfig, overrides map[string]string) (*TemplateFormatter, error) {
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

	return &TemplateFormatter{tmpl: tmpl, args: args}, nil
}

func (f *TemplateFormatter) Format(w io.Writer, artifacts []*registry.Artifact) error {
	return f.tmpl.Execute(w, Data{Artifacts: artifacts, Args: f.args})
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		// uniqueByRaw deduplicates artifacts by their Raw reference, keeping the first occurrence.
		// Useful when the same image appears from multiple sources and you only want one output entry.
		"uniqueByRaw": func(artifacts []*registry.Artifact) []*registry.Artifact {
			seen := make(map[string]bool, len(artifacts))
			out := make([]*registry.Artifact, 0, len(artifacts))
			for _, a := range artifacts {
				if !seen[a.Raw] {
					seen[a.Raw] = true
					out = append(out, a)
				}
			}
			return out
		},

		// sanitizeName replaces characters invalid in OCM/Kubernetes names with hyphens.
		"sanitizeName": func(s string) string {
			r := strings.NewReplacer("/", "-", ":", "-", "@", "-", "_", "-", ".", "-")
			return strings.Trim(r.Replace(s), "-")
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
