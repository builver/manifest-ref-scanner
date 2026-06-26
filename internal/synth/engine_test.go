package synth

import (
	"testing"

	"github.com/builver/manifest-ref-scanner/internal/config"
	"github.com/builver/manifest-ref-scanner/internal/registry"
)

// buildFluxInstance creates a FluxInstance registry.Resource with the given
// namespace, sync.url, and sync.ref values.
func buildFluxInstance(namespace, syncURL, syncRef string) *registry.Resource {
	raw := map[string]interface{}{
		"apiVersion": "fluxcd.controlplane.io/v1",
		"kind":       "FluxInstance",
		"metadata": map[string]interface{}{
			"name":      "flux",
			"namespace": namespace,
		},
		"spec": map[string]interface{}{
			"sync": map[string]interface{}{
				"url": syncURL,
				"ref": syncRef,
			},
		},
	}
	return registry.FromDoc(raw, "fluxinstance.yaml", "")
}

func TestApply_FluxInstance_SynthesizesOCIRepository(t *testing.T) {
	reg := registry.New("")
	fi := buildFluxInstance("flux-system", "oci://ghcr.io/controlplaneio-fluxcd/alpine/flux-operator-manifests", "v2.4.0")
	reg.Add(fi)

	cfg := config.DefaultConfig()
	if err := Apply(reg, cfg); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	synthetic, found := reg.Get("OCIRepository", "flux-system", "flux-system")
	if !found {
		t.Fatalf("Apply FluxInstance: expected synthetic OCIRepository/flux-system/flux-system to be in registry")
	}

	if !synthetic.Synthetic {
		t.Errorf("Apply FluxInstance: expected Synthetic=true")
	}

	if synthetic.Name != "flux-system" {
		t.Errorf("Apply FluxInstance: expected Name=flux-system, got %q", synthetic.Name)
	}

	if synthetic.Namespace != "flux-system" {
		t.Errorf("Apply FluxInstance: expected Namespace=flux-system, got %q", synthetic.Namespace)
	}

	specRaw, ok := synthetic.Raw["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("Apply FluxInstance: expected spec to be a map")
	}

	gotURL, _ := specRaw["url"].(string)
	if gotURL != "oci://ghcr.io/controlplaneio-fluxcd/alpine/flux-operator-manifests" {
		t.Errorf("Apply FluxInstance: spec.url: got %q, want %q", gotURL, "oci://ghcr.io/controlplaneio-fluxcd/alpine/flux-operator-manifests")
	}

	refMap, ok := specRaw["ref"].(map[string]interface{})
	if !ok {
		t.Fatalf("Apply FluxInstance: expected spec.ref to be a map")
	}
	gotTag, _ := refMap["tag"].(string)
	if gotTag != "v2.4.0" {
		t.Errorf("Apply FluxInstance: spec.ref.tag: got %q, want %q", gotTag, "v2.4.0")
	}
}

func TestApply_FluxInstance_SyntheticFromSet(t *testing.T) {
	reg := registry.New("")
	fi := buildFluxInstance("monitoring", "oci://ghcr.io/example/flux-manifests", "v1.0.0")
	reg.Add(fi)

	cfg := config.DefaultConfig()
	if err := Apply(reg, cfg); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	synthetic, found := reg.Get("OCIRepository", "monitoring", "flux-system")
	if !found {
		t.Fatalf("Apply FluxInstance SyntheticFrom: expected OCIRepository/monitoring/flux-system in registry")
	}

	if synthetic.SyntheticFrom == nil {
		t.Fatalf("Apply FluxInstance SyntheticFrom: expected SyntheticFrom to be set, got nil")
	}
	if synthetic.SyntheticFrom.Kind != "FluxInstance" {
		t.Errorf("Apply FluxInstance SyntheticFrom: expected Kind=FluxInstance, got %q", synthetic.SyntheticFrom.Kind)
	}
}

func TestApply_FluxInstance_NamespaceFromMetadata(t *testing.T) {
	// The synthesized OCIRepository should use the FluxInstance's namespace.
	reg := registry.New("")
	fi := buildFluxInstance("custom-ns", "oci://ghcr.io/example/manifests", "v3.0.0")
	reg.Add(fi)

	cfg := config.DefaultConfig()
	if err := Apply(reg, cfg); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	synthetic, found := reg.Get("OCIRepository", "custom-ns", "flux-system")
	if !found {
		t.Fatalf("Apply FluxInstance namespace: expected OCIRepository in namespace custom-ns")
	}
	if synthetic.Namespace != "custom-ns" {
		t.Errorf("Apply FluxInstance namespace: got %q, want custom-ns", synthetic.Namespace)
	}
}

func TestApply_NoFluxInstance_NoSynthetics(t *testing.T) {
	// Without a FluxInstance, no synthetic OCIRepository should appear.
	reg := registry.New("")
	cm := registry.FromDoc(map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "some-config",
			"namespace": "default",
		},
	}, "configmap.yaml", "")
	reg.Add(cm)

	cfg := config.DefaultConfig()
	if err := Apply(reg, cfg); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	// Registry should still only have the ConfigMap.
	all := reg.All()
	for _, r := range all {
		if r.Synthetic {
			t.Errorf("Apply no FluxInstance: unexpected synthetic resource %s/%s", r.Kind, r.Name)
		}
	}
}
