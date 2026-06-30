package walker

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/builver/manifest-ref-scanner/internal/helm"
	"github.com/builver/manifest-ref-scanner/internal/kustomize"
	"sigs.k8s.io/yaml"
)

// ParsedDoc holds a single parsed Kubernetes resource from a YAML file.
type ParsedDoc struct {
	Raw        map[string]any
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

	// Verbose prints per-subprocess timing to stderr.
	Verbose bool
}

// Walk recursively finds all *.yaml / *.yml files under root and parses every
// YAML document within them, returning the flat list of resources.
//
// The walk is a two-phase process:
//   - Phase 1: a single directory traversal collects plain YAML files, Kustomize
//     overlay directories (with their resource dependencies for leaf detection),
//     and Helm chart directories. No subprocesses are invoked here.
//   - Phase 2: plain files are parsed sequentially; kustomize and helm subprocesses
//     are launched in parallel, one goroutine per invocation.
//
// Kustomize overlay directories (containing a kustomization.yaml) are rendered
// via `kustomize build`. By default only leaf overlays are rendered — overlays
// referenced as a resource by another overlay in the same tree are skipped.
// Use KustomizeOverlayFilter to target specific overlays explicitly.
//
// Directories containing a Chart.yaml are rendered via `helm template` and
// their individual template files are not processed as plain YAML.
func Walk(root string, opts Options) ([]ParsedDoc, error) {
	log := func(format string, args ...any) {
		if opts.Verbose {
			fmt.Fprintf(os.Stderr, "[walk] "+format+"\n", args...)
		}
	}

	// Phase 1: single directory traversal — collect work items, no subprocesses.
	type overlayItem struct {
		dir   string
		kfile string
		deps  []string // absolute paths of local resource dirs this overlay depends on
	}
	type helmItem struct {
		dir  string
		name string
	}

	var (
		plainFiles []string
		overlays   []overlayItem
		helmCharts []helmItem
	)

	tWalk := time.Now()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			// Skip hidden directories (e.g. .git, .github).
			if strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return filepath.SkipDir
			}

			// Helm chart directories — collect for parallel rendering, then skip.
			if chartName, isChart, _ := helm.IsHelmChart(path); isChart {
				if !opts.DisableHelm {
					helmCharts = append(helmCharts, helmItem{dir: path, name: chartName})
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

			// Kustomize overlay directories — collect for parallel rendering, then skip.
			// Also read resource deps now so leaf detection needs no second I/O pass.
			if !opts.DisableKustomize {
				kfile, isOverlay, checkErr := kustomize.IsKustomizeDir(path)
				if checkErr != nil {
					fmt.Fprintf(os.Stderr, "warn: checking kustomize dir %s: %v\n", path, checkErr)
				} else if isOverlay {
					deps, _ := kustomize.ParseResources(path, kfile)
					overlays = append(overlays, overlayItem{dir: path, kfile: kfile, deps: deps})
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

		plainFiles = append(plainFiles, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	log("filesystem walk: %d plain files, %d overlays, %d helm charts in %s",
		len(plainFiles), len(overlays), len(helmCharts), time.Since(tWalk).Round(time.Millisecond))

	// Determine which overlays to render (leaf detection or filter matching).
	var toRender []overlayItem
	if !opts.DisableKustomize {
		if len(opts.KustomizeOverlayFilter) > 0 {
			for _, o := range overlays {
				rel, _ := filepath.Rel(root, o.dir)
				if matchesAnyGlob(rel, opts.KustomizeOverlayFilter) {
					toRender = append(toRender, o)
				}
			}
		} else {
			overlaySet := make(map[string]bool, len(overlays))
			for _, o := range overlays {
				overlaySet[filepath.Clean(o.dir)] = true
			}
			referenced := make(map[string]bool)
			for _, o := range overlays {
				for _, dep := range o.deps {
					if overlaySet[filepath.Clean(dep)] {
						referenced[filepath.Clean(dep)] = true
					}
				}
			}
			for _, o := range overlays {
				if !referenced[filepath.Clean(o.dir)] {
					toRender = append(toRender, o)
				}
			}
		}
		log("leaf detection: rendering %d of %d overlays", len(toRender), len(overlays))
	}

	// Phase 2: parse plain files sequentially (fast), render subprocesses in parallel.
	var docs []ParsedDoc

	for _, f := range plainFiles {
		fileDocs, parseErr := parseFile(f)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "warn: skipping %s: %v\n", f, parseErr)
			continue
		}
		docs = append(docs, fileDocs...)
	}

	type renderResult struct {
		docs []ParsedDoc
	}
	results := make(chan renderResult, len(toRender)+len(helmCharts))
	var wg sync.WaitGroup

	for _, o := range toRender {
		wg.Add(1)
		go func(o overlayItem) {
			defer wg.Done()
			t := time.Now()
			rendered, buildErr := kustomize.Build(o.dir)
			elapsed := time.Since(t).Round(time.Millisecond)
			if buildErr != nil {
				fmt.Fprintf(os.Stderr, "warn: kustomize build failed in %s: %v\n", o.dir, buildErr)
				results <- renderResult{}
				return
			}
			if opts.Verbose {
				fmt.Fprintf(os.Stderr, "[scan] kustomize build %s: %s\n", o.dir, elapsed)
			}
			overlayDocs, _ := parseBytes(rendered, filepath.Join(o.dir, o.kfile))
			relDir, _ := filepath.Rel(root, o.dir)
			for i := range overlayDocs {
				overlayDocs[i].KustomizeDir = relDir
			}
			results <- renderResult{docs: overlayDocs}
		}(o)
	}

	for _, h := range helmCharts {
		wg.Add(1)
		go func(h helmItem) {
			defer wg.Done()
			t := time.Now()
			rendered, buildErr := helm.Template(h.name, h.dir)
			elapsed := time.Since(t).Round(time.Millisecond)
			if buildErr != nil {
				fmt.Fprintf(os.Stderr, "warn: helm template failed in %s: %v\n", h.dir, buildErr)
				results <- renderResult{}
				return
			}
			if opts.Verbose {
				fmt.Fprintf(os.Stderr, "[scan] helm template %s: %s\n", h.dir, elapsed)
			}
			chartDocs, _ := parseBytes(rendered, filepath.Join(h.dir, "Chart.yaml"))
			results <- renderResult{docs: chartDocs}
		}(h)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		docs = append(docs, r.docs...)
	}

	return docs, nil
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
		var raw map[string]any
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
