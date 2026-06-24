package registry

import (
	"fmt"
	"strings"
)

// Registry is an in-memory store of all collected resources keyed by kind/namespace/name.
// It is built during Pass 1 and queried during Pass 2 resolution.
type Registry struct {
	resources map[string]*Resource
	// all preserves insertion order for deterministic output
	all []*Resource
}

func New() *Registry {
	return &Registry{resources: make(map[string]*Resource)}
}

// Add inserts a resource. Later entries with the same key override earlier ones,
// but a warning is printed since duplicate kind/ns/name usually indicates a bug.
func (r *Registry) Add(res *Resource) {
	key := resourceKey(res.Kind, res.Namespace, res.Name)
	if _, exists := r.resources[key]; exists {
		fmt.Printf("warn: duplicate resource %s, later entry wins\n", key)
	} else {
		r.all = append(r.all, res)
	}
	r.resources[key] = res
}

// Get looks up a resource by kind, namespace, and name.
// If namespace is empty it tries both the provided namespace and "flux-system"
// (common default for Flux resources) before giving up.
func (r *Registry) Get(kind, namespace, name string) (*Resource, bool) {
	if res, ok := r.resources[resourceKey(kind, namespace, name)]; ok {
		return res, true
	}
	// Fallback: try without namespace (cluster-scoped resources)
	if res, ok := r.resources[resourceKey(kind, "", name)]; ok {
		return res, true
	}
	// Fallback: try flux-system namespace (common Flux default)
	if namespace == "" {
		if res, ok := r.resources[resourceKey(kind, "flux-system", name)]; ok {
			return res, true
		}
	}
	return nil, false
}

// All returns every resource in insertion order.
func (r *Registry) All() []*Resource {
	return r.all
}

func resourceKey(kind, namespace, name string) string {
	return strings.ToLower(fmt.Sprintf("%s/%s/%s", kind, namespace, name))
}

// GroupFromAPIVersion extracts the group from an apiVersion string.
// "apps/v1" → "apps", "v1" → "", "source.toolkit.fluxcd.io/v1" → "source.toolkit.fluxcd.io"
func GroupFromAPIVersion(apiVersion string) string {
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) == 1 {
		return "" // core group
	}
	return parts[0]
}

// FromDoc builds a Resource from a raw parsed document.
// Returns nil if the document lacks kind/metadata.
func FromDoc(raw map[string]interface{}, sourceFile string) *Resource {
	kind, _ := raw["kind"].(string)
	apiVersion, _ := raw["apiVersion"].(string)
	if kind == "" {
		return nil
	}

	meta, _ := raw["metadata"].(map[string]interface{})
	name := ""
	namespace := ""
	if meta != nil {
		name, _ = meta["name"].(string)
		namespace, _ = meta["namespace"].(string)
	}

	return &Resource{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespace,
		Raw:        raw,
		SourceFile: sourceFile,
	}
}
