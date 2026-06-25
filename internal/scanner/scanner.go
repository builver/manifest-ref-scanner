package scanner

import (
	"github.com/patri/manifest-ref-scanner/internal/config"
	"github.com/patri/manifest-ref-scanner/internal/expander"
	"github.com/patri/manifest-ref-scanner/internal/registry"
	"github.com/patri/manifest-ref-scanner/internal/resolver"
	"github.com/patri/manifest-ref-scanner/internal/synth"
	"github.com/patri/manifest-ref-scanner/internal/walker"
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
	reg := registry.New(opts.DefaultNamespace)

	// Pass 1a: walk directory, parse all YAML files, register resources
	docs, err := walker.Walk(root, walker.Options{
		ExcludeGlobs:           opts.ExcludeGlobs,
		DisableHelm:            opts.DisableHelm,
		DisableKustomize:       opts.DisableKustomize,
		KustomizeOverlayFilter: opts.KustomizeOverlayFilter,
	})
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		if res := registry.FromDoc(doc.Raw, doc.SourceFile, doc.KustomizeDir); res != nil {
			reg.Add(res)
		}
	}

	// Pass 1b: expand inline resource templates → adds inline resources
	if err := expander.Expand(reg, cfg); err != nil {
		return nil, err
	}

	// Pass 1c: apply synthesizers → adds synthetic resources (e.g. flux-system OCIRepository)
	if err := synth.Apply(reg, cfg); err != nil {
		return nil, err
	}

	// Pass 2: resolve chains and extract OCI artifact references
	artifacts, err := resolver.Resolve(reg, cfg)
	if err != nil {
		return nil, err
	}

	return &Result{Artifacts: artifacts}, nil
}
