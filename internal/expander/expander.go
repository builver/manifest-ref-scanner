package expander

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/builver/manifest-ref-scanner/internal/config"
	"github.com/builver/manifest-ref-scanner/internal/patheval"
	"github.com/builver/manifest-ref-scanner/internal/registry"
)

// Log levels for the log function passed to Expand.
const (
	LogInfo  = 1 // -v:  overview messages (template counts, path failures)
	LogDebug = 2 // -vv: per-resource detail (each materialised child)
)

// Expand processes resources that embed inline child templates: for each configured
// expander rule, it extracts those templates and materialises one copy per input entry.
// log receives a level (LogInfo / LogDebug) so the caller decides the threshold.
func Expand(reg *registry.Registry, cfg *config.Config, log func(level int, format string, args ...any)) error {
	for _, exp := range cfg.InlineExpanders {
		if (exp.TemplateDelimLeft == "") != (exp.TemplateDelimRight == "") {
			return fmt.Errorf("inline expander %s/%s: templateDelimLeft and templateDelimRight must both be set or both be empty", exp.FromGroup, exp.FromKind)
		}
		if exp.InputPrefix != "" {
			if _, err := regexp.Compile(exp.InputPrefix); err != nil {
				return fmt.Errorf("inline expander %s/%s: invalid inputPrefix regex %q: %w", exp.FromGroup, exp.FromKind, exp.InputPrefix, err)
			}
		}
	}

	for _, res := range reg.All() {
		group := registry.GroupFromAPIVersion(res.APIVersion)
		for _, exp := range cfg.InlineExpanders {
			if exp.FromGroup != group || exp.FromKind != res.Kind {
				continue
			}
			if err := expandInline(reg, exp, res, log); err != nil {
				return err
			}
		}
	}
	return nil
}

func expandInline(reg *registry.Registry, exp config.InlineExpander, res *registry.Resource, log func(int, string, ...any)) error {
	inputs := extractList(res.Raw, exp.InputsPath)
	inputCount := len(inputs)
	if inputCount == 0 {
		inputs = []map[string]interface{}{nil} // single pass with no substitution
	}

	templates := extractList(res.Raw, exp.ResourcesPath)
	if len(templates) == 0 {
		diagnosis := diagnoseFirstFailure(res.Raw, exp.ResourcesPath)
		fmt.Fprintf(os.Stderr, "warn: expander: %s/%s/%s: resourcesPath %q → 0 templates; %s\n",
			res.Kind, res.Namespace, res.Name, exp.ResourcesPath, diagnosis)
		log(LogInfo, "%s/%s/%s: resourcesPath %q → 0 templates; %s",
			res.Kind, res.Namespace, res.Name, exp.ResourcesPath, diagnosis)
		return nil
	}
	log(LogInfo, "%s/%s/%s: %d template(s), %d input(s)", res.Kind, res.Namespace, res.Name, len(templates), inputCount)

	// Pre-compile input prefix regex (already validated in Expand).
	var keyRe *regexp.Regexp
	if exp.InputPrefix != "" {
		keyRe, _ = regexp.Compile(exp.InputPrefix)
	}

	doSubst := exp.TemplateDelimLeft != ""

	for _, input := range inputs {
		for _, tmpl := range templates {
			rendered := deepRenderMap(tmpl, input, exp.TemplateDelimLeft, exp.TemplateDelimRight, keyRe, doSubst)
			child := registry.FromDoc(rendered, res.SourceFile, "")
			if child == nil {
				continue
			}
			child.Inline = true
			child.InputContext = input
			reg.Add(child)
			log(LogDebug, "  → %s/%s/%s", child.Kind, child.Namespace, child.Name)
		}
	}
	return nil
}

// diagnoseFirstFailure walks the path segment by segment and returns the name of
// the first segment that produced no results, giving the caller a single actionable hint.
func diagnoseFirstFailure(raw map[string]interface{}, path string) string {
	segs := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	for i := 1; i <= len(segs); i++ {
		partial := strings.Join(segs[:i], "/")
		if len(patheval.GetObject(raw, partial)) == 0 {
			return fmt.Sprintf("failed at segment %q", segs[i-1])
		}
	}
	return "all segments resolved but produced no maps"
}

// extractList retrieves a flat list of maps at the given slash-separated path.
// Supports multiple [*] wildcards: each wildcard level is flattened, so a path
// like spec/pipeline[*]/input/resources[*]/template returns all template maps
// across all pipelines and resources.
func extractList(raw map[string]interface{}, path string) []map[string]interface{} {
	if path == "" {
		return nil
	}
	objs := patheval.GetObject(raw, path)
	if len(objs) == 0 {
		return nil
	}
	var result []map[string]interface{}
	for _, obj := range objs {
		switch v := obj.(type) {
		case map[string]interface{}:
			// Direct map result — from a multi-wildcard path whose final segment is a map.
			result = append(result, v)
		case []interface{}:
			// List result — from a path whose final segment is a slice (e.g. spec/resources).
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					result = append(result, m)
				}
			}
		}
	}
	return result
}

func deepRenderMap(m map[string]interface{}, input map[string]interface{}, left, right string, keyRe *regexp.Regexp, doSubst bool) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = deepRenderValue(v, input, left, right, keyRe, doSubst)
	}
	return out
}

func deepRenderValue(v interface{}, input map[string]interface{}, left, right string, keyRe *regexp.Regexp, doSubst bool) interface{} {
	switch val := v.(type) {
	case string:
		if doSubst {
			return substituteString(val, input, left, right, keyRe)
		}
		return val
	case map[string]interface{}:
		return deepRenderMap(val, input, left, right, keyRe, doSubst)
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, item := range val {
			out[i] = deepRenderValue(item, input, left, right, keyRe, doSubst)
		}
		return out
	default:
		return v
	}
}

func substituteString(s string, input map[string]interface{}, left, right string, keyRe *regexp.Regexp) string {
	// Empty delimiters would cause strings.Index to loop forever — guard here in
	// addition to the doSubst flag in expandInline for defence in depth.
	if left == "" || right == "" {
		return s
	}
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
		if v, ok := resolveKey(rawKey, input, keyRe); ok {
			repl := fmt.Sprint(v)
			s = s[:start] + repl + s[end+rightLen:]
			i = start + len(repl)
		} else {
			fmt.Fprintf(os.Stderr, "warn: expander: unresolved substitution key %q\n", rawKey)
			i = end + rightLen
		}
	}
	return s
}

// resolveKey looks up rawKey in the input map.
//
// With keyRe set (configured inputPrefix): the regex is matched at the start of
// rawKey and the matched portion is stripped. The remaining string is the lookup key.
//
// Without keyRe (default): rawKey is tried as-is first. If not found, the first
// dot-segment is stripped as a fallback (e.g. "inputs.name" → "name").
func resolveKey(rawKey string, input map[string]interface{}, keyRe *regexp.Regexp) (interface{}, bool) {
	if input == nil {
		return nil, false
	}
	if keyRe != nil {
		key := rawKey
		if loc := keyRe.FindStringIndex(rawKey); loc != nil {
			key = rawKey[loc[1]:]
		}
		v, ok := input[key]
		return v, ok
	}
	// No prefix configured: exact match first.
	if v, ok := input[rawKey]; ok {
		return v, true
	}
	// Fallback: strip first dot-segment.
	if dot := strings.IndexByte(rawKey, '.'); dot != -1 {
		v, ok := input[rawKey[dot+1:]]
		return v, ok
	}
	return nil, false
}
