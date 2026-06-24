package scanner

import (
	"github.com/patri/manifest-ref-scanner/internal/config"
	"github.com/patri/manifest-ref-scanner/internal/expander"
	"github.com/patri/manifest-ref-scanner/internal/registry"
	"github.com/patri/manifest-ref-scanner/internal/resolver"
	"github.com/patri/manifest-ref-scanner/internal/synth"
	"github.com/patri/manifest-ref-scanner/internal/walker"
)

type Result struct {
	Artifacts []*registry.Artifact
}

// Scan performs a two-pass scan of a GitOps repository root.
// Pass 1 builds a registry of all resources (real, inline, synthetic).
// Pass 2 resolves reference chains and extracts OCI artifact references.
func Scan(root string, cfg *config.Config) (*Result, error) {
	reg := registry.New()

	// Pass 1a: walk directory, parse all YAML files, register resources
	docs, err := walker.Walk(root)
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		if res := registry.FromDoc(doc.Raw, doc.SourceFile); res != nil {
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
