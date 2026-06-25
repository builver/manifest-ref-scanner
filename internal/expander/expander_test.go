package expander

import (
	"testing"

	"github.com/patri/manifest-ref-scanner/internal/config"
	"github.com/patri/manifest-ref-scanner/internal/registry"
)

// makeExpanderConfig returns a minimal config with an inline expander rule for ResourceSet.
func makeExpanderConfig() *config.Config {
	return &config.Config{
		InlineExpanders: []config.InlineExpander{
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

// buildResourceSet creates a ResourceSet registry.Resource with the given inputs and resources.
func buildResourceSet(inputs []interface{}, resources []interface{}) *registry.Resource {
	raw := map[string]interface{}{
		"apiVersion": "fluxcd.controlplane.io/v1alpha1",
		"kind":       "ResourceSet",
		"metadata": map[string]interface{}{
			"name":      "test-resourceset",
			"namespace": "flux-system",
		},
		"spec": map[string]interface{}{
			"inputs":    inputs,
			"resources": resources,
		},
	}
	return registry.FromDoc(raw, "test.yaml", "")
}

func TestExpand_NoInputs_SinglePass(t *testing.T) {
	// A ResourceSet with no inputs should produce exactly one materialized child
	// (single pass with no substitution).
	tmpl := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "my-config",
			"namespace": "default",
		},
	}
	res := buildResourceSet(nil, []interface{}{tmpl})
	reg := registry.New("")
	reg.Add(res)

	cfg := makeExpanderConfig()
	if err := Expand(reg, cfg); err != nil {
		t.Fatalf("Expand: unexpected error: %v", err)
	}

	// The child ConfigMap should be in the registry and marked as Inline.
	child, found := reg.Get("ConfigMap", "default", "my-config")
	if !found {
		t.Fatalf("Expand no inputs: expected ConfigMap/my-config to be in registry")
	}
	if !child.Inline {
		t.Errorf("Expand no inputs: expected child.Inline=true")
	}
}

func TestExpand_TwoInputs_TwoExpansions(t *testing.T) {
	// A ResourceSet with 2 inputs should produce 2 child resources.
	inputs := []interface{}{
		map[string]interface{}{"name": "alpha", "image": "img-a"},
		map[string]interface{}{"name": "beta", "image": "img-b"},
	}
	tmpl := map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":      "<< inputs.name >>",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "<< inputs.name >>",
							"image": "<< inputs.image >>",
						},
					},
				},
			},
		},
	}
	res := buildResourceSet(inputs, []interface{}{tmpl})
	reg := registry.New("")
	reg.Add(res)

	cfg := makeExpanderConfig()
	if err := Expand(reg, cfg); err != nil {
		t.Fatalf("Expand: unexpected error: %v", err)
	}

	alpha, foundAlpha := reg.Get("Deployment", "default", "alpha")
	if !foundAlpha {
		t.Errorf("Expand 2 inputs: expected Deployment/alpha in registry")
	} else if !alpha.Inline {
		t.Errorf("Expand 2 inputs: expected alpha.Inline=true")
	}

	beta, foundBeta := reg.Get("Deployment", "default", "beta")
	if !foundBeta {
		t.Errorf("Expand 2 inputs: expected Deployment/beta in registry")
	} else if !beta.Inline {
		t.Errorf("Expand 2 inputs: expected beta.Inline=true")
	}
}

func TestExpand_TemplateSubstitution(t *testing.T) {
	// Verify that << inputs.name >> is substituted with the input's "name" value.
	inputs := []interface{}{
		map[string]interface{}{"name": "myapp"},
	}
	tmpl := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "<< inputs.name >>-config",
			"namespace": "default",
		},
	}
	res := buildResourceSet(inputs, []interface{}{tmpl})
	reg := registry.New("")
	reg.Add(res)

	cfg := makeExpanderConfig()
	if err := Expand(reg, cfg); err != nil {
		t.Fatalf("Expand: unexpected error: %v", err)
	}

	child, found := reg.Get("ConfigMap", "default", "myapp-config")
	if !found {
		t.Fatalf("Expand substitution: expected ConfigMap/myapp-config in registry")
	}
	if !child.Inline {
		t.Errorf("Expand substitution: expected child.Inline=true")
	}
}

func TestExpand_UnknownKeyLeftAsIs(t *testing.T) {
	// An unknown template key should not crash and the remaining string should be preserved.
	inputs := []interface{}{
		map[string]interface{}{"name": "known"},
	}
	tmpl := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "<< inputs.name >>",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			// The key "unknown" does not exist in the input.
			"someValue": "<< inputs.unknown >>",
		},
	}
	res := buildResourceSet(inputs, []interface{}{tmpl})
	reg := registry.New("")
	reg.Add(res)

	cfg := makeExpanderConfig()
	// Should not return an error even with an unknown key.
	err := Expand(reg, cfg)
	if err != nil {
		t.Fatalf("Expand unknown key: unexpected error: %v", err)
	}

	// The known key should still be substituted.
	_, found := reg.Get("ConfigMap", "default", "known")
	if !found {
		t.Errorf("Expand unknown key: expected ConfigMap/known in registry (known key was substituted)")
	}
}

func TestExpand_InputContextSet(t *testing.T) {
	// Verify that expanded children have their InputContext populated.
	input := map[string]interface{}{"name": "ctx-test", "version": "1.0"}
	inputs := []interface{}{input}
	tmpl := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "<< inputs.name >>",
			"namespace": "default",
		},
	}
	res := buildResourceSet(inputs, []interface{}{tmpl})
	reg := registry.New("")
	reg.Add(res)

	cfg := makeExpanderConfig()
	if err := Expand(reg, cfg); err != nil {
		t.Fatalf("Expand: unexpected error: %v", err)
	}

	child, found := reg.Get("ConfigMap", "default", "ctx-test")
	if !found {
		t.Fatalf("Expand input context: expected ConfigMap/ctx-test in registry")
	}
	if child.InputContext == nil {
		t.Errorf("Expand input context: expected InputContext to be set, got nil")
	}
}
