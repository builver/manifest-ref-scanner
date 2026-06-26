package coverage

// Report holds everything the scanner could detect about potentially missed artifacts.
// UnresolvedChains and UnknownKinds are always populated. HeuristicHits is only
// populated when --coverage-output is requested (it requires a full string walk).
type Report struct {
	UnresolvedChains []UnresolvedChain `json:"unresolved_chains,omitempty" yaml:"unresolved_chains,omitempty"`
	UnknownKinds     []KindSummary     `json:"unknown_kinds,omitempty"     yaml:"unknown_kinds,omitempty"`
	HeuristicHits    []HeuristicHit    `json:"heuristic_hits,omitempty"    yaml:"heuristic_hits,omitempty"`
}

// UnresolvedChain describes a resolver chain that could not be completed because
// the referenced resource was not found in the registry.
type UnresolvedChain struct {
	Kind           string `json:"kind"                      yaml:"kind"`
	Name           string `json:"name"                      yaml:"name"`
	Namespace      string `json:"namespace,omitempty"       yaml:"namespace,omitempty"`
	ReferencedFrom string `json:"referenced_from"           yaml:"referenced_from"`
	Via            string `json:"via"                       yaml:"via"`
}

// KindSummary describes a resource kind that appeared in the scanned repository
// but has no extraction configuration.
type KindSummary struct {
	Kind  string   `json:"kind"            yaml:"kind"`
	Group string   `json:"group,omitempty" yaml:"group,omitempty"`
	Count int      `json:"count"           yaml:"count"`
	Files []string `json:"files"           yaml:"files"`
}

// HeuristicHit describes a string value that looks like an OCI reference but
// was not extracted by any configured rule.
type HeuristicHit struct {
	Value     string `json:"value"                yaml:"value"`
	Kind      string `json:"kind"                 yaml:"kind"`
	Name      string `json:"name"                 yaml:"name"`
	Namespace string `json:"namespace,omitempty"  yaml:"namespace,omitempty"`
	File      string `json:"file"                 yaml:"file"`
	FieldPath string `json:"field_path"           yaml:"field_path"`
}
