package walker

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/builver/manifest-ref-scanner/internal/helm"
	"github.com/builver/manifest-ref-scanner/internal/kustomize"
	"sigs.k8s.io/yaml"
)

// ParsedDoc holds a single parsed Kubernetes resource from a YAML file.
type ParsedDoc struct {
	Raw        map[string]interface{}
	SourceFile string
	// KustomizeDir is set when this document was produced by kustomize build.
	// It is the directory path of the overlay that was rendered.
	KustomizeDir string
}

// Options controls which files and directories the walker visits.
type Options struct {
	// ExcludeGlobs is a list of glob patterns matched against each directory's
	// base name or its path relative to the scan root. Matched directories are
	// skipped entirely. Uses filepath.Match syntax (no ** support).
	ExcludeGlobs []string

	// DisableHelm skips Helm chart rendering entirely. Chart directories are
	// skipped without a warning (their template files are not valid plain YAML).
	DisableHelm bool

	// DisableKustomize skips kustomize overlay rendering entirely. Overlay
	// directories are descended into and their individual files are processed
	// as plain YAML.
	DisableKustomize bool

	// KustomizeOverlayFilter is a list of glob patterns matched against the
	// relative path of each detected Kustomize overlay directory. When non-empty,
	// only matching overlay directories are rendered; all others are skipped
	// (their contents are not scanned). When empty, the default leaf-only
	// strategy applies.
	KustomizeOverlayFilter []string
}

// Walk recursively finds all *.yaml / *.yml files under root and parses every
// YAML document within them, returning the flat list of resources.
//
// Kustomize overlay directories (containing a kustomization.yaml with
// apiVersion kustomize.config.k8s.io/… or no apiVersion) are rendered via
// `kustomize build` and the rendered output is used in place of the individual
// files. By default only leaf overlays are rendered — overlays that are
// referenced as a resource by another overlay in the same tree are skipped
// (they are already included in the rendering of their consumers). Use
// KustomizeOverlayFilter to target specific overlays explicitly.
//
// Directories that contain a Chart.yaml are skipped entirely — Helm chart
// templates are not valid plain YAML.
func Walk(root string, opts Options) ([]ParsedDoc, error) {
	var docs []ParsedDoc

	// Pre-compute which overlay directories are leaves so the main walk can
	// decide whether to render each one. Skipped when a filter is active
	// (the filter overrides leaf logic) or kustomize is disabled.
	var leafOverlays map[string]bool
	if !opts.DisableKustomize && len(opts.KustomizeOverlayFilter) == 0 {
		leafOverlays = computeLeafOverlays(root, opts)
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			// Skip hidden directories (e.g. .git, .github)
			if strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return filepath.SkipDir
			}

			// Helm chart directories — render via `helm template` or skip silently.
			if chartName, isChart, _ := helm.IsHelmChart(path); isChart {
				if !opts.DisableHelm {
					rendered, buildErr := helm.Template(chartName, path)
					if buildErr != nil {
						fmt.Fprintf(os.Stderr, "warn: helm template failed in %s: %v\n", path, buildErr)
					} else {
						chartDocs, _ := parseBytes(rendered, filepath.Join(path, "Chart.yaml"))
						docs = append(docs, chartDocs...)
					}
				}
				return filepath.SkipDir
			}

			// Apply user-supplied exclude globs.
			rel, _ := filepath.Rel(root, path)
			for _, glob := range opts.ExcludeGlobs {
				if matchesGlob(d.Name(), rel, glob) {
					return filepath.SkipDir
				}
			}

			// Detect and render Kustomize overlays.
			if !opts.DisableKustomize {
				kfile, isOverlay, checkErr := kustomize.IsKustomizeDir(path)
				if checkErr != nil {
					fmt.Fprintf(os.Stderr, "warn: checking kustomize dir %s: %v\n", path, checkErr)
				} else if isOverlay {
					rel, _ := filepath.Rel(root, path)

					var shouldRender bool
					if len(opts.KustomizeOverlayFilter) > 0 {
						// Filter mode: render only overlay dirs that match the filter.
						shouldRender = matchesAnyGlob(rel, opts.KustomizeOverlayFilter)
					} else {
						// Default mode: render only leaf overlays (not referenced by others).
						shouldRender = leafOverlays[filepath.Clean(path)]
					}

					if shouldRender {
						rendered, buildErr := kustomize.Build(path)
						if buildErr != nil {
							fmt.Fprintf(os.Stderr, "warn: kustomize build failed in %s: %v\n", path, buildErr)
						} else {
							overlayDocs, _ := parseBytes(rendered, filepath.Join(path, kfile))
							for i := range overlayDocs {
								overlayDocs[i].KustomizeDir = path
							}
							docs = append(docs, overlayDocs...)
						}
					}
					// Always skip the directory's own files — kustomize overlays are either
					// rendered (above) or subsumed by another overlay that already includes them.
					return filepath.SkipDir
				}
			}

			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		// Also apply exclude globs to individual files.
		rel, _ := filepath.Rel(root, path)
		for _, glob := range opts.ExcludeGlobs {
			if matchesGlob(d.Name(), rel, glob) {
				return nil
			}
		}

		fileDocs, err := parseFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: skipping %s: %v\n", path, err)
			return nil
		}
		docs = append(docs, fileDocs...)
		return nil
	})

	return docs, err
}

// computeLeafOverlays does a pre-scan of root to discover all kustomize overlay
// directories and determines which are leaf overlays — i.e. not referenced as a
// resource by another overlay in the same tree. Only leaf overlays are rendered
// by default, preventing base overlays from being rendered redundantly.
func computeLeafOverlays(root string, opts Options) map[string]bool {
	type overlayEntry struct {
		dir   string // filepath.Clean'd path
		kfile string // "kustomization.yaml" or "kustomization.yml"
	}
	var overlays []overlayEntry

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error { //nolint:errcheck
		if err != nil || !d.IsDir() {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
			return filepath.SkipDir
		}
		if _, statErr := os.Stat(filepath.Join(path, "Chart.yaml")); statErr == nil {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(root, path)
		for _, glob := range opts.ExcludeGlobs {
			if matchesGlob(d.Name(), rel, glob) {
				return filepath.SkipDir
			}
		}
		kfile, isOverlay, _ := kustomize.IsKustomizeDir(path)
		if isOverlay {
			overlays = append(overlays, overlayEntry{dir: filepath.Clean(path), kfile: kfile})
			return filepath.SkipDir
		}
		return nil
	})

	// Build set of all discovered overlay dirs.
	overlaySet := make(map[string]string, len(overlays)) // clean path → kfile
	for _, o := range overlays {
		overlaySet[o.dir] = o.kfile
	}

	// Mark overlays that are referenced as a resource by another overlay.
	referenced := make(map[string]bool)
	for _, o := range overlays {
		deps, _ := kustomize.ParseResources(o.dir, o.kfile)
		for _, dep := range deps {
			clean := filepath.Clean(dep)
			if _, ok := overlaySet[clean]; ok {
				referenced[clean] = true
			}
		}
	}

	// Leaf overlays = all overlays not referenced by any other overlay.
	leafSet := make(map[string]bool, len(overlays))
	for clean := range overlaySet {
		if !referenced[clean] {
			leafSet[clean] = true
		}
	}
	return leafSet
}

// matchesGlob reports whether name (base) or rel (path relative to root)
// matches the given glob pattern. filepath.Match semantics apply.
func matchesGlob(name, rel, glob string) bool {
	if ok, _ := filepath.Match(glob, name); ok {
		return true
	}
	if ok, _ := filepath.Match(glob, rel); ok {
		return true
	}
	return false
}

// matchesAnyGlob reports whether path matches any of the given glob patterns,
// testing against both the full relative path and the base (last) segment.
func matchesAnyGlob(path string, patterns []string) bool {
	base := filepath.Base(path)
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, path); ok {
			return true
		}
		if ok, _ := filepath.Match(p, base); ok {
			return true
		}
	}
	return false
}

// parseFile reads a YAML file and splits it into individual documents.
func parseFile(path string) ([]ParsedDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseBytes(data, path)
}

// parseBytes splits raw YAML data into individual documents and unmarshals each.
// sourceFile is attached to every returned ParsedDoc as provenance.
func parseBytes(data []byte, sourceFile string) ([]ParsedDoc, error) {
	var docs []ParsedDoc
	dec := newDecoder(data)
	for {
		chunk, err := dec.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return docs, fmt.Errorf("decode: %w", err)
		}
		if len(bytes.TrimSpace(chunk)) == 0 {
			continue
		}
		var raw map[string]interface{}
		if err := yaml.Unmarshal(chunk, &raw); err != nil {
			fmt.Fprintf(os.Stderr, "warn: %s: unmarshal error: %v\n", sourceFile, err)
			continue
		}
		if raw == nil {
			continue
		}
		docs = append(docs, ParsedDoc{Raw: raw, SourceFile: sourceFile})
	}
	return docs, nil
}

// decoder splits a multi-document YAML stream on "---" boundaries.
type decoder struct {
	data   []byte
	offset int
}

func newDecoder(data []byte) *decoder { return &decoder{data: data} }

func (d *decoder) next() ([]byte, error) {
	if d.offset >= len(d.data) {
		return nil, io.EOF
	}
	rest := d.data[d.offset:]
	sep := []byte("\n---")
	idx := bytes.Index(rest, sep)
	if idx == -1 {
		d.offset = len(d.data)
		return rest, nil
	}
	chunk := rest[:idx]
	d.offset += idx + len(sep)
	if nl := bytes.IndexByte(d.data[d.offset:], '\n'); nl != -1 {
		d.offset += nl + 1
	} else {
		d.offset = len(d.data)
	}
	return chunk, nil
}
