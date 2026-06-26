package scanner

import (
	"fmt"
	"os"
	"time"

	"github.com/builver/manifest-ref-scanner/internal/config"
	"github.com/builver/manifest-ref-scanner/internal/expander"
	"github.com/builver/manifest-ref-scanner/internal/registry"
	"github.com/builver/manifest-ref-scanner/internal/resolver"
	"github.com/builver/manifest-ref-scanner/internal/synth"
	"github.com/builver/manifest-ref-scanner/internal/walker"
)

// Options controls scan behaviour.
type Options struct {
	// DefaultNamespace is used as a last-resort fallback when a resource
	// reference omits the namespace field entirely. Defaults to "default".
	DefaultNamespace string
	// ExcludeGlobs is a list of glob patterns (filepath.Match syntax) matched
	// against directory base names and paths relative to the scan root.
	// Matched directories are skipped entirely.
	ExcludeGlobs []string
	// DisableHelm skips Helm chart rendering. When true, chart directories are
	// skipped silently without rendering.
	DisableHelm bool
	// DisableKustomize skips kustomize overlay rendering. When true, directories
	// containing a Kustomize config are descended into normally and their files
	// are processed as plain YAML.
	DisableKustomize bool
	// KustomizeOverlayFilter is a list of glob patterns matched against each
	// detected Kustomize overlay directory (relative path from scan root).
	// When non-empty, only matching overlays are rendered; others are skipped.
	KustomizeOverlayFilter []string
	// Verbose prints per-phase timing to stderr.
	Verbose bool
}

// DefaultOptions returns Options with sensible defaults.
func DefaultOptions() Options {
	return Options{DefaultNamespace: "default"}
}

// Result holds the artifacts discovered by a scan.
type Result struct {
	Artifacts []*registry.Artifact
}

// Scan performs a two-pass scan of a GitOps repository root.
// Pass 1 builds a registry of all resources (real, inline, synthetic).
// Pass 2 resolves reference chains and extracts OCI artifact references.
func Scan(root string, cfg *config.Config, opts Options) (*Result, error) {
	log := func(format string, args ...any) {
		if opts.Verbose {
			fmt.Fprintf(os.Stderr, "[scan] "+format+"\n", args...)
		}
	}

	total := time.Now()
	reg := registry.New(opts.DefaultNamespace)

	// Pass 1a: walk directory, parse all YAML files, register resources
	t := time.Now()
	docs, err := walker.Walk(root, walker.Options{
		ExcludeGlobs:           opts.ExcludeGlobs,
		DisableHelm:            opts.DisableHelm,
		DisableKustomize:       opts.DisableKustomize,
		KustomizeOverlayFilter: opts.KustomizeOverlayFilter,
		Verbose:                opts.Verbose,
	})
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		if res := registry.FromDoc(doc.Raw, doc.SourceFile, doc.KustomizeDir); res != nil {
			reg.Add(res)
		}
	}
	log("pass1a walk+parse: %d docs registered in %s", len(docs), time.Since(t).Round(time.Millisecond))

	// Pass 1b: expand inline resource templates → adds inline resources
	t = time.Now()
	if err := expander.Expand(reg, cfg); err != nil {
		return nil, err
	}
	log("pass1b expander:   %s", time.Since(t).Round(time.Millisecond))

	// Pass 1c: apply synthesizers → adds synthetic resources (e.g. flux-system OCIRepository)
	t = time.Now()
	if err := synth.Apply(reg, cfg); err != nil {
		return nil, err
	}
	log("pass1c synth:      %s", time.Since(t).Round(time.Millisecond))

	// Pass 2: resolve chains and extract OCI artifact references
	t = time.Now()
	artifacts, err := resolver.Resolve(reg, cfg)
	if err != nil {
		return nil, err
	}
	log("pass2  resolver:   %d artifacts in %s", len(artifacts), time.Since(t).Round(time.Millisecond))
	log("total:             %s", time.Since(total).Round(time.Millisecond))

	return &Result{Artifacts: artifacts}, nil
}
