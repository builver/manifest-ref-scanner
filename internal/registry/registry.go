package registry

import (
	"fmt"
	"os"
	"strings"
)

// Registry is an in-memory store of all collected resources keyed by kind/namespace/name.
// It is built during Pass 1 and queried during Pass 2 resolution.
type Registry struct {
	resources        map[string]*Resource
	all              []*Resource // preserves insertion order for deterministic output
	DefaultNamespace string      // used as last-resort fallback when a ref omits namespace
}

// New creates an empty Registry. defaultNamespace is tried as a last-resort
// namespace when a reference field omits the namespace entirely.
// Pass "" to disable the fallback (lookups will only try the exact namespace
// and the cluster-scoped empty-namespace key).
func New(defaultNamespace string) *Registry {
	return &Registry{
		resources:        make(map[string]*Resource),
		DefaultNamespace: defaultNamespace,
	}
}

// Add inserts a resource. When two resources share the same kind/namespace/name
// but come from different kustomize overlays, both are kept in the iteration
// list so each overlay's version gets independently resolved — this is expected
// when multiple overlays render the same resource with different field values.
// Any other duplicate (same source or plain-file collision) logs a warning and
// the later entry wins in the lookup map.
func (r *Registry) Add(res *Resource) {
	key := resourceKey(res.Kind, res.Namespace, res.Name)
	existing, exists := r.resources[key]
	r.resources[key] = res

	if !exists {
		r.all = append(r.all, res)
		return
	}

	// Different kustomize overlays may render the same resource kind/name with
	// different values (e.g. staging vs production image tags). Keep both in
	// r.all so the resolver extracts refs from each independently.
	if existing.KustomizeDir != "" && res.KustomizeDir != "" && existing.KustomizeDir != res.KustomizeDir {
		r.all = append(r.all, res)
		return
	}

	fmt.Fprintf(os.Stderr, "warn: duplicate resource %s, later entry wins\n", key)
}

// Get looks up a resource by kind, namespace, and name.
// Fallback order:
//  1. Exact kind/namespace/name
//  2. Cluster-scoped (empty namespace)
//  3. DefaultNamespace (if the caller did not supply a namespace)
func (r *Registry) Get(kind, namespace, name string) (*Resource, bool) {
	if res, ok := r.resources[resourceKey(kind, namespace, name)]; ok {
		return res, true
	}
	// Cluster-scoped resources have no namespace.
	if res, ok := r.resources[resourceKey(kind, "", name)]; ok {
		return res, true
	}
	// When the reference omits a namespace, try the configured default.
	if namespace == "" && r.DefaultNamespace != "" {
		if res, ok := r.resources[resourceKey(kind, r.DefaultNamespace, name)]; ok {
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
