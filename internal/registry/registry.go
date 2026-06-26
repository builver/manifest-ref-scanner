package registry

import (
	"fmt"
	"strings"
)

// Registry is an in-memory store of all collected resources keyed by kind/namespace/name.
// It is built during Pass 1 and queried during Pass 2 resolution.
type Registry struct {
	byKey            map[string][]*Resource
	all              []*Resource // preserves insertion order for deterministic output
	DefaultNamespace string      // used as last-resort fallback when a ref omits namespace
}

// New creates an empty Registry. defaultNamespace is tried as a last-resort
// namespace when a reference field omits the namespace entirely.
// Pass "" to disable the fallback (lookups will only try the exact namespace
// and the cluster-scoped empty-namespace key).
func New(defaultNamespace string) *Registry {
	return &Registry{
		byKey:            make(map[string][]*Resource),
		DefaultNamespace: defaultNamespace,
	}
}

// Add inserts a resource. Every resource is stored regardless of duplicates;
// ambiguity between multiple resources sharing the same kind/namespace/name is
// handled at query time by GetAll, not at insertion time.
func (r *Registry) Add(res *Resource) {
	key := resourceKey(res.Kind, res.Namespace, res.Name)
	r.byKey[key] = append(r.byKey[key], res)
	r.all = append(r.all, res)
}

// Get returns one resource for the given key using the namespace fallback chain
// described on GetAll. When multiple resources share the key (e.g. the same
// resource name rendered by two different kustomize overlays), the last-registered
// one is returned. Callers that need all versions should use GetAll instead.
func (r *Registry) Get(kind, namespace, name string) (*Resource, bool) {
	all := r.GetAll(kind, namespace, name)
	if len(all) == 0 {
		return nil, false
	}
	return all[len(all)-1], true
}

// GetAll returns every resource matching kind/namespace/name, including
// resources from different kustomize overlays or source files.
// Fallback order when no exact match is found:
//  1. Exact kind/namespace/name
//  2. Cluster-scoped (empty namespace)
//  3. DefaultNamespace (only when namespace was omitted by the caller)
func (r *Registry) GetAll(kind, namespace, name string) []*Resource {
	if res := r.byKey[resourceKey(kind, namespace, name)]; len(res) > 0 {
		return res
	}
	// Cluster-scoped resources have no namespace.
	if res := r.byKey[resourceKey(kind, "", name)]; len(res) > 0 {
		return res
	}
	// When the reference omits a namespace, try the configured default.
	if namespace == "" && r.DefaultNamespace != "" {
		if res := r.byKey[resourceKey(kind, r.DefaultNamespace, name)]; len(res) > 0 {
			return res
		}
	}
	return nil
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
// kustomizeDir is the overlay directory that produced the document via kustomize build;
// pass "" for resources loaded directly from files.
// Returns nil if the document lacks kind/metadata.
func FromDoc(raw map[string]interface{}, sourceFile, kustomizeDir string) *Resource {
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
	// Resources without a name cannot be looked up by resolver chains and are
	// not useful for OCI ref extraction (e.g. Kustomize config files parsed as
	// plain YAML when --no-kustomize is active).
	if name == "" {
		return nil
	}

	return &Resource{
		APIVersion:   apiVersion,
		Kind:         kind,
		Name:         name,
		Namespace:    namespace,
		Raw:          raw,
		SourceFile:   sourceFile,
		KustomizeDir: kustomizeDir,
	}
}
