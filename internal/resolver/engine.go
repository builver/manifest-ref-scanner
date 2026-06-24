package resolver

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/patri/manifest-ref-scanner/internal/config"
	"github.com/patri/manifest-ref-scanner/internal/patheval"
	"github.com/patri/manifest-ref-scanner/internal/refparser"
	"github.com/patri/manifest-ref-scanner/internal/registry"
)

// Resolve runs Pass 2 against the fully-populated registry.
// Step A extracts OCI refs directly from resources that carry them.
// Step B follows resolver chains (e.g. Kustomization → OCIRepository).
// Results are deduplicated by raw ref + field type, keeping the longest chain.
func Resolve(reg *registry.Registry, cfg *config.Config) ([]*registry.Artifact, error) {
	var artifacts []*registry.Artifact

	for _, res := range reg.All() {
		group := registry.GroupFromAPIVersion(res.APIVersion)

		// Step A: direct field extraction
		for _, ft := range cfg.FieldTypes {
			for _, target := range ft.Targets {
				if !matchesTarget(group, res.Kind, target) {
					continue
				}
				arts := extractFromTarget(res, ft.Name, target)
				artifacts = append(artifacts, arts...)
			}
		}

		// Step B: follow resolver chains
		for _, r := range cfg.Resolvers {
			if r.FromGroup != group || r.FromKind != res.Kind {
				continue
			}
			arts, err := followResolver(reg, cfg, res, r, nil)
			if err != nil {
				fmt.Printf("warn: resolver %s on %s/%s: %v\n", r.Name, res.Kind, res.Name, err)
			}
			artifacts = append(artifacts, arts...)
		}
	}

	return dedup(artifacts), nil
}

func followResolver(
	reg *registry.Registry,
	cfg *config.Config,
	res *registry.Resource,
	r config.Resolver,
	chain []registry.ResolutionStep,
) ([]*registry.Artifact, error) {
	refObjs := patheval.GetObject(res.Raw, r.Path)
	if len(refObjs) == 0 {
		return nil, nil
	}
	refMap, ok := refObjs[0].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	kind := renderField(r.Resolves.Kind, refMap)
	name := renderField(r.Resolves.Name, refMap)
	namespace := renderField(r.Resolves.Namespace, refMap)
	if namespace == "" {
		namespace = res.Namespace
	}

	step := registry.ResolutionStep{
		Kind:        res.Kind,
		Name:        res.Name,
		Namespace:   res.Namespace,
		File:        res.SourceFile,
		Via:         r.Path,
		Synthesized: res.Synthetic,
		Inline:      res.Inline,
		Input:       res.InputContext,
	}
	chain = append(chain, step)

	target, found := reg.Get(kind, namespace, name)
	if !found {
		art := &registry.Artifact{
			FieldType: "unresolved",
			Raw:       fmt.Sprintf("%s/%s/%s", kind, namespace, name),
			Resolution: chain,
			Warnings:  []string{fmt.Sprintf("could not find %s/%s in registry", kind, name)},
		}
		return []*registry.Artifact{art}, nil
	}

	var artifacts []*registry.Artifact
	targetGroup := registry.GroupFromAPIVersion(target.APIVersion)

	for _, ft := range cfg.FieldTypes {
		for _, tgt := range ft.Targets {
			if !matchesTarget(targetGroup, target.Kind, tgt) {
				continue
			}
			arts := extractFromTargetWithChain(target, ft.Name, tgt, chain)
			artifacts = append(artifacts, arts...)
		}
	}

	if len(chain) < 10 {
		for _, r2 := range cfg.Resolvers {
			if r2.FromGroup != targetGroup || r2.FromKind != target.Kind {
				continue
			}
			arts, _ := followResolver(reg, cfg, target, r2, chain)
			artifacts = append(artifacts, arts...)
		}
	}

	return artifacts, nil
}

func extractFromTarget(res *registry.Resource, fieldType string, target config.FieldTarget) []*registry.Artifact {
	return extractFromTargetWithChain(res, fieldType, target, nil)
}

func extractFromTargetWithChain(
	res *registry.Resource,
	fieldType string,
	target config.FieldTarget,
	chain []registry.ResolutionStep,
) []*registry.Artifact {
	step := registry.ResolutionStep{
		Kind:        res.Kind,
		Name:        res.Name,
		Namespace:   res.Namespace,
		File:        res.SourceFile,
		Synthesized: res.Synthetic,
		Inline:      res.Inline,
		Input:       res.InputContext,
	}
	fullChain := append(append([]registry.ResolutionStep{}, chain...), step)

	var arts []*registry.Artifact

	switch {
	case target.NamePath != "":
		// Fully split: separate name and tag paths
		names := patheval.Get(res.Raw, target.NamePath)
		for i, name := range names {
			tag := ""
			for _, tp := range target.TagPaths {
				vals := patheval.Get(res.Raw, tp)
				if i < len(vals) && vals[i] != "" {
					tag = vals[i]
					break
				}
			}
			arts = append(arts, buildArtifact(fieldType, combinedRef(name, tag), tag, nil, fullChain))
		}

	case target.Path != "" && len(target.TagPaths) > 0:
		// URL in Path, tag resolved from TagPaths in order (first non-empty wins)
		names := patheval.Get(res.Raw, target.Path)
		for i, name := range names {
			tag := ""
			for _, tp := range target.TagPaths {
				vals := patheval.Get(res.Raw, tp)
				if i < len(vals) && vals[i] != "" {
					tag = vals[i]
					break
				}
			}
			arts = append(arts, buildArtifact(fieldType, combinedRef(name, tag), tag, nil, fullChain))
		}

	case target.Path != "":
		// Fully merged ref in a single field
		for _, raw := range patheval.Get(res.Raw, target.Path) {
			arts = append(arts, buildArtifact(fieldType, raw, "", nil, fullChain))
		}
	}

	return arts
}

func combinedRef(name, tag string) string {
	if tag != "" {
		return name + ":" + tag
	}
	return name
}

func buildArtifact(fieldType, raw, tagHint string, extraRef map[string]string, chain []registry.ResolutionStep) *registry.Artifact {
	art := &registry.Artifact{
		FieldType:  fieldType,
		Raw:        raw,
		Ref:        extraRef,
		Resolution: chain,
	}
	ref, err := refparser.Parse(raw)
	if err == nil {
		art.Registry   = ref.Registry
		art.Repository = ref.Repository
		art.Tag        = ref.Tag
		art.Digest     = ref.Digest
	} else {
		if tagHint != "" {
			art.Tag = tagHint
		}
		art.Warnings = append(art.Warnings, fmt.Sprintf("could not parse ref: %v", err))
	}
	return art
}

func matchesTarget(group, kind string, target config.FieldTarget) bool {
	if target.Kind != kind {
		return false
	}
	return target.Group == "" || target.Group == group
}

// renderField evaluates a simple Go template against a string-keyed map.
// A missing key returns "" (not Go template's "<no value>").
func renderField(tmpl string, data map[string]interface{}) string {
	t, err := template.New("").Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	t.Execute(&buf, data)
	result := buf.String()
	if result == "<no value>" {
		return ""
	}
	return result
}

// dedup collapses artifacts with identical (raw, fieldType) keys, keeping
// the entry with the longest resolution chain (richest context).
func dedup(arts []*registry.Artifact) []*registry.Artifact {
	type key struct{ raw, fieldType string }
	best := make(map[key]*registry.Artifact, len(arts))
	var order []key

	for _, art := range arts {
		k := key{raw: art.Raw, fieldType: art.FieldType}
		if existing, ok := best[k]; !ok {
			order = append(order, k)
			best[k] = art
		} else if len(art.Resolution) > len(existing.Resolution) {
			best[k] = art
		}
	}

	result := make([]*registry.Artifact, 0, len(order))
	for _, k := range order {
		result = append(result, best[k])
	}
	return result
}
