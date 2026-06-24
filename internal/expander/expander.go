package expander

import (
	"fmt"
	"os"
	"strings"

	"github.com/patri/manifest-ref-scanner/internal/config"
	"github.com/patri/manifest-ref-scanner/internal/patheval"
	"github.com/patri/manifest-ref-scanner/internal/registry"
)

// Expand processes resources that embed inline child templates: for each configured
// expander rule, it extracts those templates and materializes one copy per input entry.
func Expand(reg *registry.Registry, cfg *config.Config) error {
	for _, res := range reg.All() {
		group := registry.GroupFromAPIVersion(res.APIVersion)
		for _, exp := range cfg.InlineExpanders {
			if exp.FromGroup != group || exp.FromKind != res.Kind {
				continue
			}
			if err := expandInline(reg, exp, res); err != nil {
				return err
			}
		}
	}
	return nil
}

func expandInline(reg *registry.Registry, exp config.InlineExpander, res *registry.Resource) error {
	inputs := extractList(res.Raw, exp.InputsPath)
	if len(inputs) == 0 {
		inputs = []map[string]interface{}{nil} // single pass with no substitution
	}

	templates := extractList(res.Raw, exp.ResourcesPath)

	for _, input := range inputs {
		for _, tmpl := range templates {
			rendered := renderResourceTemplate(tmpl, input, exp.TemplateDelimLeft, exp.TemplateDelimRight)
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

// extractList retrieves a list of maps at the given slash-separated path.
func extractList(raw map[string]interface{}, path string) []map[string]interface{} {
	objs := patheval.GetObject(raw, path)
	if len(objs) == 0 {
		return nil
	}
	// The value at the path is a []interface{} slice.
	list, ok := objs[0].([]interface{})
	if !ok {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]interface{}); ok {
			result = append(result, m)
		}
	}
	return result
}

// renderResourceTemplate replaces left+key+right occurrences in all string values
// of the map using the input map. Keys use the "inputs.name" prefix convention:
// "inputs.name" → input["name"].
func renderResourceTemplate(tmpl map[string]interface{}, input map[string]interface{}, left, right string) map[string]interface{} {
	return deepRenderMap(tmpl, input, left, right)
}

func deepRenderMap(m map[string]interface{}, input map[string]interface{}, left, right string) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = deepRenderValue(v, input, left, right)
	}
	return out
}

func deepRenderValue(v interface{}, input map[string]interface{}, left, right string) interface{} {
	switch val := v.(type) {
	case string:
		return substituteString(val, input, left, right)
	case map[string]interface{}:
		return deepRenderMap(val, input, left, right)
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, item := range val {
			out[i] = deepRenderValue(item, input, left, right)
		}
		return out
	default:
		return v
	}
}

func substituteString(s string, input map[string]interface{}, left, right string) string {
	leftLen := len(left)
	rightLen := len(right)
	i := 0
	for {
		start := strings.Index(s[i:], left)
		if start == -1 {
			break
		}
		start += i

		end := strings.Index(s[start+leftLen:], right)
		if end == -1 {
			break
		}
		end = start + leftLen + end

		rawKey := strings.TrimSpace(s[start+leftLen : end])
		key := strings.TrimPrefix(rawKey, "inputs.")

		if input != nil {
			if v, ok := input[key]; ok {
				replacement := fmt.Sprint(v)
				s = s[:start] + replacement + s[end+rightLen:]
				i = start + len(replacement)
				continue
			}
		}
		// Unknown key: emit a warning and skip past this occurrence.
		fmt.Fprintf(os.Stderr, "warn: expander: unresolved substitution key %q\n", rawKey)
		i = end + rightLen
	}
	return s
}
