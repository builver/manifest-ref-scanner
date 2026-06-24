package synth

import (
	"bytes"
	"text/template"

	"github.com/patri/manifest-ref-scanner/internal/config"
	"github.com/patri/manifest-ref-scanner/internal/registry"
)

// Apply runs all synthesizer rules against the current registry contents and
// registers each resulting synthetic resource.
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
	name, err := renderTemplate(s.Emits.Name, src.Raw)
	if err != nil {
		return nil, err
	}
	namespace, err := renderTemplate(s.Emits.Namespace, src.Raw)
	if err != nil {
		return nil, err
	}

	raw := map[string]interface{}{
		"apiVersion": s.Emits.APIVersion,
		"kind":       s.Emits.Kind,
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
		"spec": renderSpecMap(s.Emits.Spec, src.Raw),
	}

	return &registry.Resource{
		APIVersion: s.Emits.APIVersion,
		Kind:       s.Emits.Kind,
		Name:       name,
		Namespace:  namespace,
		Raw:        raw,
		Synthetic:  true,
		SyntheticFrom: &registry.ResourceRef{
			Kind:      src.Kind,
			Name:      src.Name,
			Namespace: src.Namespace,
			File:      src.SourceFile,
		},
	}, nil
}

// renderSpecMap recursively walks a spec template map and renders string leaf
// values as Go templates against data.
func renderSpecMap(tmpl map[string]interface{}, data interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(tmpl))
	for k, v := range tmpl {
		switch val := v.(type) {
		case string:
			rendered, _ := renderTemplate(val, data)
			out[k] = rendered
		case map[string]interface{}:
			out[k] = renderSpecMap(val, data)
		default:
			out[k] = v
		}
	}
	return out
}

func renderTemplate(tmpl string, data interface{}) (string, error) {
	t, err := template.New("").Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
