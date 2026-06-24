package patheval

import "strings"

// Get returns all string values at the slash-separated path.
// A segment with a [*] suffix iterates all elements of a slice.
func Get(obj map[string]interface{}, path string) []string {
	raw := walkAny([]interface{}{obj}, splitPath(path))
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// GetObject returns raw interface{} values at the slash-separated path.
func GetObject(obj map[string]interface{}, path string) []interface{} {
	return walkAny([]interface{}{obj}, splitPath(path))
}

// Set writes value at the slash-separated path, creating intermediate maps as needed.
func Set(obj map[string]interface{}, path string, value interface{}) {
	segs := splitPath(path)
	cur := obj
	for i, seg := range segs {
		if i == len(segs)-1 {
			cur[seg] = value
			return
		}
		next, ok := cur[seg].(map[string]interface{})
		if !ok {
			next = make(map[string]interface{})
			cur[seg] = next
		}
		cur = next
	}
}

func splitPath(path string) []string {
	return strings.Split(path, "/")
}

func walkAny(current []interface{}, segs []string) []interface{} {
	if len(segs) == 0 || len(current) == 0 {
		return current
	}
	seg := segs[0]
	rest := segs[1:]

	isSlice := strings.HasSuffix(seg, "[*]")
	key := strings.TrimSuffix(seg, "[*]")

	var next []interface{}
	for _, item := range current {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		val, exists := m[key]
		if !exists {
			continue
		}
		if isSlice {
			slice, ok := val.([]interface{})
			if !ok {
				continue
			}
			next = append(next, slice...)
		} else {
			next = append(next, val)
		}
	}
	return walkAny(next, rest)
}
