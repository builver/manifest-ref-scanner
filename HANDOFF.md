# manifest-ref-scanner — AI Handoff Document

## What This Is

A Go CLI tool that statically scans a Kubernetes GitOps repository and extracts all OCI artifact references. It is aware of standard K8s resource specs, Flux CRDs, and is extensible to arbitrary CRDs via a config file similar to kustomize's `nameReference.yaml`.

Key design properties:
- Two-pass architecture: Pass 1 collects resources into an in-memory registry; Pass 2 resolves reference chains
- Handles **implicit resource creation**: `FluxInstance.spec.sync` → synthesizes an `OCIRepository/flux-system` that never exists in any YAML file
- Handles **indirect reference chains**: `Kustomization.spec.sourceRef.name: flux-system` → follows to OCIRepository → extracts the OCI URL+tag
- Handles **inline resources in ResourceSets**: extracts `spec.resources[]` as real resources and materializes them per `spec.inputs[]` entry
- Separates OCI image name from tag (supports split `namePath`/`tagPath` fields)
- Outputs YAML with a full resolution chain per artifact

## Module

```
module github.com/builver/manifest-ref-scanner
go 1.25
```

Dependencies already fetched:
- `sigs.k8s.io/yaml`
- `github.com/spf13/cobra`
- `github.com/distribution/reference` (OCI ref parsing)
- `github.com/opencontainers/go-digest`

---

## Project Structure

```
manifest-ref-scanner/
├── main.go                          ← TODO
├── go.mod / go.sum                  ← DONE
├── cmd/
│   └── root.go                      ← TODO
├── internal/
│   ├── config/
│   │   ├── types.go                 ← DONE
│   │   └── defaults.go              ← DONE
│   ├── walker/
│   │   └── walker.go                ← DONE
│   ├── registry/
│   │   ├── types.go                 ← DONE
│   │   └── registry.go              ← DONE
│   ├── patheval/
│   │   └── patheval.go              ← TODO (see spec below)
│   ├── synth/
│   │   └── engine.go                ← TODO (see spec below)
│   ├── expander/
│   │   └── expander.go              ← TODO (see spec below)
│   ├── resolver/
│   │   └── engine.go                ← TODO (see spec below)
│   ├── refparser/
│   │   └── refparser.go             ← TODO (see spec below)
│   ├── scanner/
│   │   └── scanner.go               ← TODO (see spec below)
│   └── output/
│       └── output.go                ← TODO (see spec below)
```

---

## Files Already Written (full content)

### internal/config/types.go

```go
package config

type Config struct {
	FieldTypes           []FieldType           `yaml:"fieldTypes"`
	Synthesizers         []Synthesizer         `yaml:"synthesizers"`
	Resolvers            []Resolver            `yaml:"resolvers"`
	ResourceSetExpanders []ResourceSetExpander `yaml:"resourceSetExpanders"`
}

type FieldType struct {
	Name    string        `yaml:"name"`
	Targets []FieldTarget `yaml:"targets"`
}

type FieldTarget struct {
	Group    string `yaml:"group"`
	Kind     string `yaml:"kind"`
	Path     string `yaml:"path"`     // e.g. spec/containers[*]/image
	NamePath string `yaml:"namePath"` // e.g. spec/image/repository
	TagPath  string `yaml:"tagPath"`  // e.g. spec/image/tag
}

type Synthesizer struct {
	Name      string            `yaml:"name"`
	FromGroup string            `yaml:"fromGroup"`
	FromKind  string            `yaml:"fromKind"`
	Emits     SynthesizedObject `yaml:"emits"`
}

type SynthesizedObject struct {
	APIVersion string                 `yaml:"apiVersion"`
	Kind       string                 `yaml:"kind"`
	Name       string                 `yaml:"name"`      // Go template
	Namespace  string                 `yaml:"namespace"` // Go template
	Spec       map[string]interface{} `yaml:"spec"`      // nested Go templates as string values
}

type Resolver struct {
	Name      string        `yaml:"name"`
	FromGroup string        `yaml:"fromGroup"`
	FromKind  string        `yaml:"fromKind"`
	Path      string        `yaml:"path"` // path to the ref object, e.g. spec/sourceRef
	Resolves  ResolveTarget `yaml:"resolves"`
}

type ResolveTarget struct {
	Kind      string `yaml:"kind"`
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

type ResourceSetExpander struct {
	FromGroup          string `yaml:"fromGroup"`
	FromKind           string `yaml:"fromKind"`
	ResourcesPath      string `yaml:"resourcesPath"`
	InputsPath         string `yaml:"inputsPath"`
	TemplateDelimLeft  string `yaml:"templateDelimLeft"`
	TemplateDelimRight string `yaml:"templateDelimRight"`
}
```

### internal/config/defaults.go

```go
package config

func DefaultConfig() *Config {
	return &Config{
		FieldTypes: []FieldType{
			{
				Name: "containerImage",
				Targets: []FieldTarget{
					{Kind: "Pod", Path: "spec/containers[*]/image"},
					{Kind: "Pod", Path: "spec/initContainers[*]/image"},
					{Kind: "Pod", Path: "spec/ephemeralContainers[*]/image"},
					{Kind: "Deployment", Path: "spec/template/spec/containers[*]/image"},
					{Kind: "Deployment", Path: "spec/template/spec/initContainers[*]/image"},
					{Kind: "DaemonSet", Path: "spec/template/spec/containers[*]/image"},
					{Kind: "DaemonSet", Path: "spec/template/spec/initContainers[*]/image"},
					{Kind: "StatefulSet", Path: "spec/template/spec/containers[*]/image"},
					{Kind: "StatefulSet", Path: "spec/template/spec/initContainers[*]/image"},
					{Kind: "ReplicaSet", Path: "spec/template/spec/containers[*]/image"},
					{Kind: "Job", Path: "spec/template/spec/containers[*]/image"},
					{Kind: "Job", Path: "spec/template/spec/initContainers[*]/image"},
					{Kind: "CronJob", Path: "spec/jobTemplate/spec/template/spec/containers[*]/image"},
					{Kind: "CronJob", Path: "spec/jobTemplate/spec/template/spec/initContainers[*]/image"},
				},
			},
			{
				Name: "ociArtifact",
				Targets: []FieldTarget{
					{Group: "source.toolkit.fluxcd.io", Kind: "OCIRepository", Path: "spec/url", TagPath: "spec/ref/tag"},
					{Group: "fluxcd.controlplane.io", Kind: "FluxInstance", Path: "spec/distribution/artifact"},
					{Group: "source.toolkit.fluxcd.io", Kind: "HelmRepository", Path: "spec/url"},
				},
			},
		},
		Synthesizers: []Synthesizer{
			{
				Name:      "fluxInstanceSync",
				FromGroup: "fluxcd.controlplane.io",
				FromKind:  "FluxInstance",
				// FluxInstance.spec.sync causes the Flux Operator to create an
				// OCIRepository named "flux-system" in the same namespace.
				Emits: SynthesizedObject{
					APIVersion: "source.toolkit.fluxcd.io/v1",
					Kind:       "OCIRepository",
					Name:       "flux-system",
					Namespace:  "{{.metadata.namespace}}",
					Spec: map[string]interface{}{
						"url": "{{.spec.sync.url}}",
						"ref": map[string]interface{}{
							"tag": "{{.spec.sync.ref}}",
						},
					},
				},
			},
		},
		Resolvers: []Resolver{
			{
				Name:      "kustomizationSourceRef",
				FromGroup: "kustomize.toolkit.fluxcd.io",
				FromKind:  "Kustomization",
				Path:      "spec/sourceRef",
				Resolves:  ResolveTarget{Kind: "{{.kind}}", Name: "{{.name}}", Namespace: "{{.namespace}}"},
			},
			{
				Name:      "helmReleaseChartRef",
				FromGroup: "helm.toolkit.fluxcd.io",
				FromKind:  "HelmRelease",
				Path:      "spec/chartRef",
				Resolves:  ResolveTarget{Kind: "{{.kind}}", Name: "{{.name}}", Namespace: "{{.namespace}}"},
			},
			{
				Name:      "helmReleaseSourceRef",
				FromGroup: "helm.toolkit.fluxcd.io",
				FromKind:  "HelmRelease",
				Path:      "spec/chart/spec/sourceRef",
				Resolves:  ResolveTarget{Kind: "{{.kind}}", Name: "{{.name}}", Namespace: "{{.namespace}}"},
			},
		},
		ResourceSetExpanders: []ResourceSetExpander{
			{
				FromGroup:          "fluxcd.controlplane.io",
				FromKind:           "ResourceSet",
				ResourcesPath:      "spec/resources",
				InputsPath:         "spec/inputs",
				TemplateDelimLeft:  "<<",
				TemplateDelimRight: ">>",
			},
		},
	}
}
```

### internal/registry/types.go

```go
package registry

type Resource struct {
	APIVersion    string
	Kind          string
	Name          string
	Namespace     string
	Raw           map[string]interface{}
	SourceFile    string
	Inline        bool
	Synthetic     bool
	SyntheticFrom *ResourceRef
	InputContext  map[string]interface{}
}

type ResourceRef struct {
	Kind      string
	Namespace string
	Name      string
	File      string
}

type Artifact struct {
	FieldType  string            `yaml:"fieldType"`
	Raw        string            `yaml:"raw"`
	Registry   string            `yaml:"registry,omitempty"`
	Repository string            `yaml:"repository,omitempty"`
	Tag        string            `yaml:"tag,omitempty"`
	Digest     string            `yaml:"digest,omitempty"`
	Ref        map[string]string `yaml:"ref,omitempty"`
	Resolution []ResolutionStep  `yaml:"resolution"`
	Warnings   []string          `yaml:"warnings,omitempty"`
}

type ResolutionStep struct {
	Kind        string                 `yaml:"kind"`
	Name        string                 `yaml:"name"`
	Namespace   string                 `yaml:"namespace,omitempty"`
	File        string                 `yaml:"file,omitempty"`
	Via         string                 `yaml:"via,omitempty"`
	Synthesized bool                   `yaml:"synthesized,omitempty"`
	Inline      bool                   `yaml:"inline,omitempty"`
	Input       map[string]interface{} `yaml:"input,omitempty"`
}
```

### internal/registry/registry.go

```go
package registry

import (
	"fmt"
	"strings"
)

type Registry struct {
	resources map[string]*Resource
	all       []*Resource
}

func New() *Registry {
	return &Registry{resources: make(map[string]*Resource)}
}

func (r *Registry) Add(res *Resource) {
	key := resourceKey(res.Kind, res.Namespace, res.Name)
	if _, exists := r.resources[key]; exists {
		fmt.Printf("warn: duplicate resource %s, later entry wins\n", key)
	} else {
		r.all = append(r.all, res)
	}
	r.resources[key] = res
}

func (r *Registry) Get(kind, namespace, name string) (*Resource, bool) {
	if res, ok := r.resources[resourceKey(kind, namespace, name)]; ok {
		return res, true
	}
	if res, ok := r.resources[resourceKey(kind, "", name)]; ok {
		return res, true
	}
	if namespace == "" {
		if res, ok := r.resources[resourceKey(kind, "flux-system", name)]; ok {
			return res, true
		}
	}
	return nil, false
}

func (r *Registry) All() []*Resource { return r.all }

func resourceKey(kind, namespace, name string) string {
	return strings.ToLower(fmt.Sprintf("%s/%s/%s", kind, namespace, name))
}

func GroupFromAPIVersion(apiVersion string) string {
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) == 1 {
		return ""
	}
	return parts[0]
}

func FromDoc(raw map[string]interface{}, sourceFile string) *Resource {
	kind, _ := raw["kind"].(string)
	apiVersion, _ := raw["apiVersion"].(string)
	if kind == "" {
		return nil
	}
	meta, _ := raw["metadata"].(map[string]interface{})
	name, namespace := "", ""
	if meta != nil {
		name, _ = meta["name"].(string)
		namespace, _ = meta["namespace"].(string)
	}
	return &Resource{
		APIVersion: apiVersion, Kind: kind,
		Name: name, Namespace: namespace,
		Raw: raw, SourceFile: sourceFile,
	}
}
```

### internal/walker/walker.go

```go
package walker

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

type ParsedDoc struct {
	Raw        map[string]interface{}
	SourceFile string
}

func Walk(root string) ([]ParsedDoc, error) {
	var docs []ParsedDoc
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
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

func parseFile(path string) ([]ParsedDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var docs []ParsedDoc
	dec := newDecoder(data)
	for {
		var raw map[string]interface{}
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
		if err := yaml.Unmarshal(chunk, &raw); err != nil {
			fmt.Fprintf(os.Stderr, "warn: %s: unmarshal error: %v\n", path, err)
			continue
		}
		if raw == nil {
			continue
		}
		docs = append(docs, ParsedDoc{Raw: raw, SourceFile: path})
	}
	return docs, nil
}

type decoder struct{ data []byte; offset int }

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
```

---

## TODO: Remaining Files to Write

Continue writing exactly these files in order. After each file, verify the project compiles with `go build ./...` before moving on.

---

### internal/patheval/patheval.go

Package `patheval` evaluates slash-separated field paths like `spec/containers[*]/image` against an unstructured `map[string]interface{}`.

Rules:
- `/` is the path separator
- `[*]` suffix on a key means "value is a slice, iterate all elements"
- Return all matching string values from `Get()`
- `GetObject()` returns raw `interface{}` values (used to extract ref objects like sourceRef)
- `Set()` sets a value at a path, creating intermediate maps — used by synthesizers

```go
package patheval

import "strings"

func Get(obj map[string]interface{}, path string) []string {
    // split path on "/", then walkAny, collect strings
}

func GetObject(obj map[string]interface{}, path string) []interface{} {
    // same but return raw interface{} values
}

func Set(obj map[string]interface{}, path string, value interface{}) {
    // walk path creating maps, set final key
}

func splitPath(path string) []string { /* split on "/" */ }

func walkAny(current []interface{}, segs []string) []interface{} {
    // recursive: peel one segment, check for [*], recurse
}
```

---

### internal/refparser/refparser.go

Package `refparser` parses OCI reference strings into components using `github.com/distribution/reference`.

Strip leading `oci://` before parsing if present.

```go
package refparser

import "github.com/distribution/reference"

type Ref struct {
    Raw        string
    Registry   string
    Repository string
    Tag        string
    Digest     string
}

func Parse(raw string) (*Ref, error) {
    // strip "oci://" prefix
    // call reference.ParseNormalizedNamed
    // extract tag via reference.Tagged interface
    // extract digest via reference.Digested interface
    // split registry from repository using reference.Domain / reference.Path
}
```

---

### internal/synth/engine.go

Package `synth` applies `config.Synthesizer` rules to resources in the registry.

For each resource in the registry, check if any synthesizer matches (`FromGroup` + `FromKind`). If it matches, build the synthetic resource using Go text/template with the source resource's `Raw` map as template data. Register the synthetic resource back into the registry.

```go
package synth

import (
    "bytes"
    "text/template"
    "github.com/builver/manifest-ref-scanner/internal/config"
    "github.com/builver/manifest-ref-scanner/internal/registry"
)

func Apply(reg *registry.Registry, cfg *config.Config) error {
    for _, res := range reg.All() {
        group := registry.GroupFromAPIVersion(res.APIVersion)
        for _, s := range cfg.Synthesizers {
            if s.FromGroup != group || s.FromKind != res.Kind {
                continue
            }
            synthetic, err := buildSynthetic(s, res)
            if err != nil {
                return err
            }
            reg.Add(synthetic)
        }
    }
    return nil
}

func buildSynthetic(s config.Synthesizer, src *registry.Resource) (*registry.Resource, error) {
    // renderTemplate renders a Go template string against src.Raw
    name, _ := renderTemplate(s.Emits.Name, src.Raw)
    namespace, _ := renderTemplate(s.Emits.Namespace, src.Raw)

    // Build the raw map for the synthetic resource
    raw := map[string]interface{}{
        "apiVersion": s.Emits.APIVersion,
        "kind":       s.Emits.Kind,
        "metadata": map[string]interface{}{
            "name":      name,
            "namespace": namespace,
        },
        "spec": renderSpecTemplates(s.Emits.Spec, src.Raw),
    }

    return &registry.Resource{
        APIVersion: s.Emits.APIVersion,
        Kind:       s.Emits.Kind,
        Name:       name,
        Namespace:  namespace,
        Raw:        raw,
        Synthetic:  true,
        SyntheticFrom: &registry.ResourceRef{
            Kind: src.Kind, Name: src.Name,
            Namespace: src.Namespace, File: src.SourceFile,
        },
    }, nil
}

// renderSpecTemplates recursively walks the spec template map and renders
// any string values as Go templates.
func renderSpecTemplates(tmpl map[string]interface{}, data interface{}) map[string]interface{} { ... }

func renderTemplate(tmpl string, data interface{}) (string, error) {
    t, err := template.New("").Option("missingkey=zero").Parse(tmpl)
    if err != nil { return "", err }
    var buf bytes.Buffer
    err = t.Execute(&buf, data)
    return buf.String(), err
}
```

**Important**: The Go template data for synth is the raw map of the source resource. So `{{.metadata.namespace}}` accesses `raw["metadata"].(map)["namespace"]`. Since `raw` is `map[string]interface{}`, the template engine handles dot-access natively.

Actually, for nested map access in Go templates, use a helper or pre-flatten. The simplest approach: pass `src.Raw` directly as data — Go templates support `{{index . "spec" "sync" "url"}}` but NOT `{{.spec.sync.url}}` on maps. **Use `{{index . "spec" "sync" "url"}}` syntax** OR convert the raw map to a struct, OR use a FuncMap with a `get` helper.

Recommended: add a FuncMap with `get` that does map lookup:
```go
funcMap := template.FuncMap{
    "get": func(m map[string]interface{}, key string) interface{} { return m[key] },
}
```
And write the template strings in defaults.go using a helper function `nestedGet(raw, "spec", "sync", "url")` instead of Go templates. The simplest correct approach: **use `patheval.Get()` directly in buildSynthetic** instead of templates for extracting values, and only use Go template for the output name/namespace strings.

Revised simpler approach for buildSynthetic:
```go
func buildSynthetic(s config.Synthesizer, src *registry.Resource) (*registry.Resource, error) {
    name := renderSimple(s.Emits.Name, src)   // only replaces {{.metadata.name}} etc via simple strings
    namespace := renderSimple(s.Emits.Namespace, src)
    // Build spec by rendering the spec template map using patheval for value extraction
    // The Spec map in SynthesizedObject has Go template strings as leaf values
    spec := renderSpecMap(s.Emits.Spec, src.Raw)
    ...
}
```

---

### internal/expander/expander.go

Package `expander` extracts inline resources from ResourceSets and materializes them per input.

```go
package expander

import (
    "fmt"
    "strings"
    "github.com/builver/manifest-ref-scanner/internal/config"
    "github.com/builver/manifest-ref-scanner/internal/patheval"
    "github.com/builver/manifest-ref-scanner/internal/registry"
    "sigs.k8s.io/yaml"
)

func Expand(reg *registry.Registry, cfg *config.Config) error {
    for _, res := range reg.All() {
        group := registry.GroupFromAPIVersion(res.APIVersion)
        for _, exp := range cfg.ResourceSetExpanders {
            if exp.FromGroup != group || exp.FromKind != res.Kind {
                continue
            }
            if err := expandResourceSet(reg, exp, res); err != nil {
                return err
            }
        }
    }
    return nil
}

func expandResourceSet(reg *registry.Registry, exp config.ResourceSetExpander, res *registry.Resource) error {
    // 1. Extract spec.inputs — list of maps
    inputs := extractInputs(res.Raw, exp.InputsPath)
    if len(inputs) == 0 {
        inputs = []map[string]interface{}{nil} // single pass with no substitution
    }

    // 2. Extract spec.resources — list of raw resource maps
    rawResources := extractResources(res.Raw, exp.ResourcesPath)

    for _, input := range inputs {
        for _, tmpl := range rawResources {
            // 3. Render << >> templates in the resource using the input values
            rendered := renderResourceTemplate(tmpl, input, exp.TemplateDelimLeft, exp.TemplateDelimRight)

            // 4. Register as inline resource
            child := registry.FromDoc(rendered, res.SourceFile)
            if child == nil {
                continue
            }
            child.Inline = true
            child.InputContext = input
            reg.Add(child)
        }
    }
    return nil
}

// renderResourceTemplate replaces << key >> with input[key] in all string values of the map.
// Non-resolvable substitutions (e.g. ${ENVIRONMENT}) are left as-is with a warning.
func renderResourceTemplate(tmpl map[string]interface{}, input map[string]interface{}, left, right string) map[string]interface{} {
    // Deep-clone and walk all string values, replacing left+key+right with input[key]
    ...
}
```

For the `<< inputs.name >>` pattern: the substitution key is `inputs.name`. Strip the `inputs.` prefix, look up `input["name"]`.

---

### internal/resolver/engine.go

Package `resolver` runs Pass 2: for each resource and each configured resolver, follows the reference to the target resource and extracts OCI artifacts via the field types.

```go
package resolver

import (
    "fmt"
    "github.com/builver/manifest-ref-scanner/internal/config"
    "github.com/builver/manifest-ref-scanner/internal/patheval"
    "github.com/builver/manifest-ref-scanner/internal/refparser"
    "github.com/builver/manifest-ref-scanner/internal/registry"
)

func Resolve(reg *registry.Registry, cfg *config.Config) ([]*registry.Artifact, error) {
    var artifacts []*registry.Artifact

    // Step A: Direct field extraction — no reference following needed.
    // For each resource, check all FieldType targets that match its kind/group.
    // Extract OCI refs directly from the resource's own fields.
    for _, res := range reg.All() {
        group := registry.GroupFromAPIVersion(res.APIVersion)
        for _, ft := range cfg.FieldTypes {
            for _, target := range ft.Targets {
                if !matchesTarget(group, res.Kind, target) {
                    continue
                }
                arts := extractFromTarget(res, ft.Name, target, cfg)
                artifacts = append(artifacts, arts...)
            }
        }
    }

    // Step B: Reference following — for each Resolver rule, find matching resources,
    // follow the reference to the target, extract artifacts from the target.
    // Artifacts found via resolution get the full chain prepended.
    for _, res := range reg.All() {
        group := registry.GroupFromAPIVersion(res.APIVersion)
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

func followResolver(reg *registry.Registry, cfg *config.Config, res *registry.Resource, r config.Resolver, chain []registry.ResolutionStep) ([]*registry.Artifact, error) {
    // 1. Get the ref object at r.Path (e.g. spec/sourceRef → {kind: OCIRepository, name: flux-system})
    refObjs := patheval.GetObject(res.Raw, r.Path)
    if len(refObjs) == 0 {
        return nil, nil
    }
    refMap, ok := refObjs[0].(map[string]interface{})
    if !ok {
        return nil, nil
    }

    // 2. Build lookup key from the ResolveTarget templates
    kind := renderField(r.Resolves.Kind, refMap)
    name := renderField(r.Resolves.Name, refMap)
    namespace := renderField(r.Resolves.Namespace, refMap)
    if namespace == "" {
        namespace = res.Namespace
    }

    // 3. Prepend current resource to chain
    step := registry.ResolutionStep{
        Kind: res.Kind, Name: res.Name, Namespace: res.Namespace,
        File: res.SourceFile, Via: r.Path,
        Synthesized: res.Synthetic, Inline: res.Inline,
        Input: res.InputContext,
    }
    chain = append(chain, step)

    // 4. Look up target in registry
    target, found := reg.Get(kind, namespace, name)
    if !found {
        // Emit a warning artifact
        art := &registry.Artifact{
            FieldType: "unresolved",
            Raw:       fmt.Sprintf("%s/%s/%s", kind, namespace, name),
            Resolution: chain,
            Warnings: []string{fmt.Sprintf("could not find %s/%s in registry", kind, name)},
        }
        return []*registry.Artifact{art}, nil
    }

    // 5. Try to extract OCI refs from target directly
    var artifacts []*registry.Artifact
    targetGroup := registry.GroupFromAPIVersion(target.APIVersion)
    for _, ft := range cfg.FieldTypes {
        for _, tgt := range ft.Targets {
            if !matchesTarget(targetGroup, target.Kind, tgt) {
                continue
            }
            arts := extractFromTargetWithChain(target, ft.Name, tgt, cfg, chain)
            artifacts = append(artifacts, arts...)
        }
    }

    // 6. If target itself has resolvers, follow transitively (depth limit: 10)
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

func extractFromTarget(res *registry.Resource, fieldType string, target config.FieldTarget, cfg *config.Config) []*registry.Artifact {
    return extractFromTargetWithChain(res, fieldType, target, cfg, nil)
}

func extractFromTargetWithChain(res *registry.Resource, fieldType string, target config.FieldTarget, cfg *config.Config, chain []registry.ResolutionStep) []*registry.Artifact {
    var arts []*registry.Artifact

    step := registry.ResolutionStep{
        Kind: res.Kind, Name: res.Name, Namespace: res.Namespace,
        File: res.SourceFile,
        Synthesized: res.Synthetic, Inline: res.Inline,
        Input: res.InputContext,
    }
    fullChain := append(chain, step)

    if target.Path != "" && target.TagPath == "" {
        // Merged ref: full "registry/repo:tag" in one field
        for _, raw := range patheval.Get(res.Raw, target.Path) {
            art := buildArtifact(fieldType, raw, "", fullChain)
            arts = append(arts, art)
        }
    } else if target.Path != "" && target.TagPath != "" {
        // Name-only path + separate tag path
        names := patheval.Get(res.Raw, target.Path)
        tags := patheval.Get(res.Raw, target.TagPath)
        for i, name := range names {
            tag := ""
            if i < len(tags) {
                tag = tags[i]
            }
            combined := name
            if tag != "" {
                combined = name + ":" + tag
            }
            art := buildArtifact(fieldType, combined, tag, fullChain)
            arts = append(arts, art)
        }
    } else if target.NamePath != "" {
        // Fully split name/tag
        names := patheval.Get(res.Raw, target.NamePath)
        tags := patheval.Get(res.Raw, target.TagPath)
        for i, name := range names {
            tag := ""
            if i < len(tags) {
                tag = tags[i]
            }
            combined := name
            if tag != "" {
                combined = name + ":" + tag
            }
            art := buildArtifact(fieldType, combined, tag, fullChain)
            arts = append(arts, art)
        }
    }

    return arts
}

func buildArtifact(fieldType, raw, tagHint string, chain []registry.ResolutionStep) *registry.Artifact {
    art := &registry.Artifact{
        FieldType:  fieldType,
        Raw:        raw,
        Resolution: chain,
    }
    ref, err := refparser.Parse(raw)
    if err == nil {
        art.Registry   = ref.Registry
        art.Repository = ref.Repository
        art.Tag        = ref.Tag
        art.Digest     = ref.Digest
    } else if tagHint != "" {
        art.Tag = tagHint
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

// renderField replaces simple {{.key}} patterns in template strings using the map.
func renderField(tmpl string, data map[string]interface{}) string { ... }

func dedup(arts []*registry.Artifact) []*registry.Artifact {
    // deduplicate by Raw+FieldType, keeping the one with the longest resolution chain
    ...
}
```

---

### internal/refparser/refparser.go

```go
package refparser

import (
    "strings"
    "github.com/distribution/reference"
)

type Ref struct {
    Raw        string
    Registry   string
    Repository string
    Tag        string
    Digest     string
}

func Parse(raw string) (*Ref, error) {
    s := strings.TrimPrefix(raw, "oci://")
    named, err := reference.ParseNormalizedNamed(s)
    if err != nil {
        return nil, err
    }
    named = reference.TagNameOnly(named)
    r := &Ref{
        Raw:        raw,
        Registry:   reference.Domain(named),
        Repository: reference.Path(named),
    }
    if tagged, ok := named.(reference.Tagged); ok {
        r.Tag = tagged.Tag()
    }
    if digested, ok := named.(reference.Digested); ok {
        r.Digest = digested.Digest().String()
    }
    return r, nil
}
```

---

### internal/scanner/scanner.go

Orchestrates the two passes:

```go
package scanner

import (
    "github.com/builver/manifest-ref-scanner/internal/config"
    "github.com/builver/manifest-ref-scanner/internal/expander"
    "github.com/builver/manifest-ref-scanner/internal/registry"
    "github.com/builver/manifest-ref-scanner/internal/resolver"
    "github.com/builver/manifest-ref-scanner/internal/synth"
    "github.com/builver/manifest-ref-scanner/internal/walker"
)

type Result struct {
    Artifacts []*registry.Artifact
}

func Scan(root string, cfg *config.Config) (*Result, error) {
    reg := registry.New()

    // --- Pass 1: Collect ---
    // 1a. Walk directory, parse all YAML files, register resources
    docs, err := walker.Walk(root)
    if err != nil {
        return nil, err
    }
    for _, doc := range docs {
        res := registry.FromDoc(doc.Raw, doc.SourceFile)
        if res != nil {
            reg.Add(res)
        }
    }

    // 1b. Expand ResourceSets (adds inline resources to registry)
    if err := expander.Expand(reg, cfg); err != nil {
        return nil, err
    }

    // 1c. Apply synthesizers (adds synthetic resources like flux-system OCIRepository)
    if err := synth.Apply(reg, cfg); err != nil {
        return nil, err
    }

    // --- Pass 2: Resolve ---
    artifacts, err := resolver.Resolve(reg, cfg)
    if err != nil {
        return nil, err
    }

    return &Result{Artifacts: artifacts}, nil
}
```

---

### internal/output/output.go

```go
package output

import (
    "io"
    "github.com/builver/manifest-ref-scanner/internal/registry"
    "sigs.k8s.io/yaml"
)

type Report struct {
    Artifacts []*registry.Artifact `yaml:"artifacts"`
}

func WriteYAML(w io.Writer, artifacts []*registry.Artifact) error {
    report := Report{Artifacts: artifacts}
    data, err := yaml.Marshal(report)
    if err != nil {
        return err
    }
    _, err = w.Write(data)
    return err
}
```

---

### cmd/root.go

```go
package cmd

import (
    "fmt"
    "os"
    "github.com/spf13/cobra"
    "github.com/builver/manifest-ref-scanner/internal/config"
    "github.com/builver/manifest-ref-scanner/internal/output"
    "github.com/builver/manifest-ref-scanner/internal/scanner"
)

var (
    cfgFile    string
    outputFile string
)

var rootCmd = &cobra.Command{
    Use:   "manifest-ref-scanner [path]",
    Short: "Scan a Kubernetes GitOps repository for OCI artifact references",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        cfg := config.DefaultConfig()

        // TODO: if cfgFile != "", merge user config over defaults

        result, err := scanner.Scan(args[0], cfg)
        if err != nil {
            return fmt.Errorf("scan: %w", err)
        }

        w := os.Stdout
        if outputFile != "" {
            f, err := os.Create(outputFile)
            if err != nil {
                return err
            }
            defer f.Close()
            w = f
        }

        return output.WriteYAML(w, result.Artifacts)
    },
}

func Execute() {
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}

func init() {
    rootCmd.Flags().StringVarP(&cfgFile, "config", "c", "", "path to refs config file (extends built-ins)")
    rootCmd.Flags().StringVarP(&outputFile, "output", "o", "", "write output to file (default: stdout)")
}
```

### main.go

```go
package main

import "github.com/builver/manifest-ref-scanner/cmd"

func main() { cmd.Execute() }
```

---

## Build & Test

```bash
cd /home/patri/git/manifest-ref-scanner
go build ./...
go run . /home/patri/git/learn/learn-gitless-gitops
```

Expected output for the learn-gitless-gitops repo:
- `oci://ghcr.io/builver/learn-gitless-gitops:latest` (from FluxInstance.sync → synthesized OCIRepository)
- `oci://ghcr.io/controlplaneio-fluxcd/flux-operator-manifests:latest` (from FluxInstance.distribution.artifact)
- `oci://ghcr.io/controlplaneio-fluxcd/charts/flux-operator` (from ResourceSet inline OCIRepository, ref.semver=*)
- `ghcr.io/stefanprodan/podinfo` + tag (from podinfo HelmRelease or Deployment if present)

Each artifact should have a resolution chain showing how it was discovered.

---

## Key Design Decisions to Preserve

1. **Two-pass**: always collect ALL resources before resolving. Synthesizers run after all real resources are loaded.
2. **Synthesizer for FluxInstance**: the `flux-system` OCIRepository is NEVER in any YAML file — it's inferred from `FluxInstance.spec.sync`. This is the most important edge case.
3. **ResourceSet expansion**: inline resources (spec.resources[]) must be extracted and registered before resolution — they are the source of the `flux-operator` OCIRepository in this repo.
4. **Resolver transitive follow**: a Kustomization points to an OCIRepository; the resolver follows that chain. The OCIRepository itself is the terminal node that has the OCI URL.
5. **Dedup in output**: the same OCIRepository might be reached via multiple Kustomizations — deduplicate by raw ref value, keep the richest resolution chain.
6. **Unresolved refs are warnings, not errors**: if `sourceRef.name: foo` has no matching resource, emit a warning artifact rather than failing.
