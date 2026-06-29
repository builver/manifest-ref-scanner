package scanner

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/builver/manifest-ref-scanner/internal/config"
	"github.com/builver/manifest-ref-scanner/internal/coverage"
	"github.com/builver/manifest-ref-scanner/internal/expander"
	"github.com/builver/manifest-ref-scanner/internal/heuristic"
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
	// Verbosity controls how much diagnostic output is printed to stderr.
	// 0 = silent, 1 = phase timings (-v), 2 = per-resource detail (-vv).
	Verbosity int
	// CoverageOutput is the file path to write the coverage report to.
	// When empty, no coverage report is produced.
	CoverageOutput string
}

// DefaultOptions returns Options with sensible defaults.
func DefaultOptions() Options {
	return Options{DefaultNamespace: "default"}
}

// Result holds the artifacts discovered by a scan.
type Result struct {
	Artifacts []*registry.Artifact
	// Coverage is populated only when Options.CoverageOutput is non-empty.
	Coverage *coverage.Report
}

// Scan performs a two-pass scan of a GitOps repository root.
// Pass 1 builds a registry of all resources (real, inline, synthetic).
// Pass 2 resolves reference chains and extracts OCI artifact references.
func Scan(root string, cfg *config.Config, opts Options) (*Result, error) {
	log := func(format string, args ...any) {
		if opts.Verbosity >= 1 {
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
		Verbose:                opts.Verbosity >= 1,
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
	expandLog := func(level int, format string, args ...any) {
		if level <= opts.Verbosity {
			fmt.Fprintf(os.Stderr, "[expander] "+format+"\n", args...)
		}
	}
	if err := expander.Expand(reg, cfg, expandLog); err != nil {
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
	artifacts, unresolved, err := resolver.Resolve(reg, cfg)
	if err != nil {
		return nil, err
	}
	log("pass2  resolver:   %d artifacts in %s", len(artifacts), time.Since(t).Round(time.Millisecond))
	log("total:             %s", time.Since(total).Round(time.Millisecond))

	cov := buildCoverage(reg, cfg, unresolved, artifacts)
	if opts.CoverageOutput != "" {
		enrichCoverage(cov, reg, artifacts)
	}

	return &Result{Artifacts: artifacts, Coverage: cov}, nil
}

// buildCoverage computes the coverage report.
// UnresolvedChains and UnknownKinds are always computed (cheap, already in memory).
// HeuristicHits are only computed when coverageOutput is set (string regex walk).
func buildCoverage(
	reg *registry.Registry,
	cfg *config.Config,
	unresolved []coverage.UnresolvedChain,
	artifacts []*registry.Artifact,
) *coverage.Report {
	return &coverage.Report{
		UnresolvedChains: unresolved,
		UnknownKinds:     unknownKinds(reg, cfg),
	}
}

// enrichCoverage adds the heuristic string scan results to an existing report.
func enrichCoverage(rep *coverage.Report, reg *registry.Registry, artifacts []*registry.Artifact) {
	knownRefs := make(map[string]bool, len(artifacts))
	for _, a := range artifacts {
		knownRefs[a.Reference] = true
	}
	rep.HeuristicHits = heuristic.Scan(reg.All(), knownRefs)
}

// unknownKinds returns KindSummary entries for every kind+group combination
// found in the registry that has no configuration in any FieldType target,
// Resolver source, Synthesizer source/output, or InlineExpander source,
// and is not listed in SuppressedKinds.
func unknownKinds(reg *registry.Registry, cfg *config.Config) []coverage.KindSummary {
	type kindKey struct{ kind, group string }

	configured := make(map[kindKey]bool)
	for _, ft := range cfg.FieldTypes {
		for _, tgt := range ft.Targets {
			configured[kindKey{tgt.Kind, tgt.Group}] = true
		}
	}
	for _, r := range cfg.Resolvers {
		configured[kindKey{r.FromKind, r.FromGroup}] = true
	}
	for _, s := range cfg.Synthesizers {
		configured[kindKey{s.FromKind, s.FromGroup}] = true
		configured[kindKey{s.Emits.Kind, ""}] = true
	}
	for _, ie := range cfg.InlineExpanders {
		configured[kindKey{ie.FromKind, ie.FromGroup}] = true
	}

	// suppressedWildcard holds kinds with no Group specified — they match any API group.
	// suppressedExact holds kinds with an explicit Group (including "" for core/v1).
	suppressedWildcard := make(map[string]bool)
	suppressedExact := make(map[kindKey]bool)
	for _, sk := range cfg.SuppressedKinds {
		if sk.Group == nil {
			suppressedWildcard[sk.Kind] = true
		} else {
			suppressedExact[kindKey{sk.Kind, *sk.Group}] = true
		}
	}

	type entry struct {
		count int
		files map[string]bool
	}
	byKind := make(map[kindKey]*entry)
	for _, res := range reg.All() {
		if res.Synthetic {
			continue
		}
		group := registry.GroupFromAPIVersion(res.APIVersion)
		k := kindKey{res.Kind, group}
		if configured[k] || suppressedWildcard[k.kind] || suppressedExact[k] {
			continue
		}
		e := byKind[k]
		if e == nil {
			e = &entry{files: make(map[string]bool)}
			byKind[k] = e
		}
		e.count++
		if res.SourceFile != "" {
			e.files[res.SourceFile] = true
		}
	}

	if len(byKind) == 0 {
		return nil
	}

	summaries := make([]coverage.KindSummary, 0, len(byKind))
	for k, e := range byKind {
		files := make([]string, 0, len(e.files))
		for f := range e.files {
			files = append(files, f)
		}
		sort.Strings(files)
		summaries = append(summaries, coverage.KindSummary{
			Kind:  k.kind,
			Group: k.group,
			Count: e.count,
			Files: files,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Kind != summaries[j].Kind {
			return summaries[i].Kind < summaries[j].Kind
		}
		return summaries[i].Group < summaries[j].Group
	})
	return summaries
}
