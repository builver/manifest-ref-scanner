package config

// Config is the top-level configuration for the scanner.
type Config struct {
	FieldTypes           []FieldType           `yaml:"fieldTypes"`
	Synthesizers         []Synthesizer         `yaml:"synthesizers"`
	Resolvers            []Resolver            `yaml:"resolvers"`
	ResourceSetExpanders []ResourceSetExpander `yaml:"resourceSetExpanders"`
}

// FieldType defines a named category of reference (e.g. "containerImage")
// and the field paths within specific resource kinds where that reference lives.
type FieldType struct {
	Name    string        `yaml:"name"`
	Targets []FieldTarget `yaml:"targets"`
}

// FieldTarget maps a resource kind to the field path(s) containing the reference.
// Use Path for a fully merged "registry/repo:tag" value.
// Use NamePath + TagPath when the image name and tag live in separate fields.
type FieldTarget struct {
	Group    string `yaml:"group"`    // empty string = core group
	Kind     string `yaml:"kind"`
	Path     string `yaml:"path"`     // e.g. spec/containers[*]/image
	NamePath string `yaml:"namePath"` // e.g. spec/image/repository
	TagPath  string `yaml:"tagPath"`  // e.g. spec/image/tag
}

// Synthesizer declares that a resource implicitly creates another resource at runtime.
// The canonical example is FluxInstance.spec.sync → OCIRepository/flux-system.
type Synthesizer struct {
	Name      string            `yaml:"name"`
	FromGroup string            `yaml:"fromGroup"`
	FromKind  string            `yaml:"fromKind"`
	Emits     SynthesizedObject `yaml:"emits"`
}

// SynthesizedObject is a Go-template-driven resource skeleton.
// Templates have access to the source resource's fields via {{.spec.sync.url}} etc.
type SynthesizedObject struct {
	APIVersion string                 `yaml:"apiVersion"`
	Kind       string                 `yaml:"kind"`
	Name       string                 `yaml:"name"`      // Go template
	Namespace  string                 `yaml:"namespace"` // Go template
	Spec       map[string]interface{} `yaml:"spec"`      // nested Go templates as string values
}

// Resolver teaches the scanner how to follow a reference field (sourceRef, chartRef, etc.)
// from one resource to another so the chain can be walked transitively.
type Resolver struct {
	Name      string        `yaml:"name"`
	FromGroup string        `yaml:"fromGroup"`
	FromKind  string        `yaml:"fromKind"`
	Path      string        `yaml:"path"` // path to the reference object, e.g. spec/sourceRef
	Resolves  ResolveTarget `yaml:"resolves"`
}

// ResolveTarget describes how to construct the lookup key for the referenced resource.
// The fields are Go templates evaluated against the object found at Resolver.Path.
type ResolveTarget struct {
	Kind      string `yaml:"kind"`      // e.g. "{{ .kind }}"
	Name      string `yaml:"name"`      // e.g. "{{ .name }}"
	Namespace string `yaml:"namespace"` // e.g. "{{ .namespace | default fromNamespace }}"
}

// ResourceSetExpander teaches the scanner how to extract inline resource templates
// from a ResourceSet-style resource and materialize them per input.
type ResourceSetExpander struct {
	FromGroup          string `yaml:"fromGroup"`
	FromKind           string `yaml:"fromKind"`
	ResourcesPath      string `yaml:"resourcesPath"`      // e.g. spec/resources
	InputsPath         string `yaml:"inputsPath"`         // e.g. spec/inputs
	TemplateDelimLeft  string `yaml:"templateDelimLeft"`  // e.g. "<<"
	TemplateDelimRight string `yaml:"templateDelimRight"` // e.g. ">>"
}
