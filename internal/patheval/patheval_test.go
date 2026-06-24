package patheval

import (
	"reflect"
	"testing"
)

func TestGet_NestedMaps(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"url": "oci://ghcr.io/example/repo:v1",
		},
	}
	got := Get(obj, "spec/url")
	want := []string{"oci://ghcr.io/example/repo:v1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Get nested map: got %v, want %v", got, want)
	}
}

func TestGet_SliceIteration(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"containers": []interface{}{
				map[string]interface{}{"image": "nginx:1.0"},
				map[string]interface{}{"image": "redis:7"},
			},
		},
	}
	got := Get(obj, "spec/containers[*]/image")
	want := []string{"nginx:1.0", "redis:7"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Get slice iteration: got %v, want %v", got, want)
	}
}

func TestGet_MissingKey(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"url": "oci://ghcr.io/example/repo:v1",
		},
	}
	got := Get(obj, "spec/notExist")
	if len(got) != 0 {
		t.Errorf("Get missing key: expected empty slice, got %v", got)
	}
}

func TestGet_MissingIntermediateKey(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{},
	}
	got := Get(obj, "spec/ref/tag")
	if len(got) != 0 {
		t.Errorf("Get missing intermediate key: expected empty slice, got %v", got)
	}
}

func TestGet_EmptySlice(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"containers": []interface{}{},
		},
	}
	got := Get(obj, "spec/containers[*]/image")
	if len(got) != 0 {
		t.Errorf("Get empty slice: expected empty, got %v", got)
	}
}

func TestGet_DeepNested(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"ref": map[string]interface{}{
				"tag": "v2.3.1",
			},
		},
	}
	got := Get(obj, "spec/ref/tag")
	want := []string{"v2.3.1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Get deep nested: got %v, want %v", got, want)
	}
}

func TestGetObject_ReturnsRawValues(t *testing.T) {
	inner := map[string]interface{}{"kind": "OCIRepository", "name": "flux-system"}
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"sourceRef": inner,
		},
	}
	got := GetObject(obj, "spec/sourceRef")
	if len(got) != 1 {
		t.Fatalf("GetObject: expected 1 result, got %d", len(got))
	}
	m, ok := got[0].(map[string]interface{})
	if !ok {
		t.Fatalf("GetObject: expected map, got %T", got[0])
	}
	if m["kind"] != "OCIRepository" {
		t.Errorf("GetObject: expected kind=OCIRepository, got %v", m["kind"])
	}
	if m["name"] != "flux-system" {
		t.Errorf("GetObject: expected name=flux-system, got %v", m["name"])
	}
}

func TestGetObject_SliceValue(t *testing.T) {
	slice := []interface{}{
		map[string]interface{}{"name": "a"},
		map[string]interface{}{"name": "b"},
	}
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"inputs": slice,
		},
	}
	got := GetObject(obj, "spec/inputs")
	if len(got) != 1 {
		t.Fatalf("GetObject slice: expected 1 result (the slice itself), got %d", len(got))
	}
	gotSlice, ok := got[0].([]interface{})
	if !ok {
		t.Fatalf("GetObject slice: expected []interface{}, got %T", got[0])
	}
	if len(gotSlice) != 2 {
		t.Errorf("GetObject slice: expected 2 elements, got %d", len(gotSlice))
	}
}

func TestGetObject_Missing(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{},
	}
	got := GetObject(obj, "spec/sourceRef")
	if len(got) != 0 {
		t.Errorf("GetObject missing: expected empty, got %v", got)
	}
}

func TestSet_CreatesIntermediateMaps(t *testing.T) {
	obj := map[string]interface{}{}
	Set(obj, "spec/ref/tag", "v1.0.0")

	spec, ok := obj["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("Set: expected spec to be a map, got %T", obj["spec"])
	}
	ref, ok := spec["ref"].(map[string]interface{})
	if !ok {
		t.Fatalf("Set: expected spec/ref to be a map, got %T", spec["ref"])
	}
	if ref["tag"] != "v1.0.0" {
		t.Errorf("Set: expected spec/ref/tag=v1.0.0, got %v", ref["tag"])
	}
}

func TestSet_OverwritesExisting(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"url": "oci://old",
		},
	}
	Set(obj, "spec/url", "oci://new")
	spec := obj["spec"].(map[string]interface{})
	if spec["url"] != "oci://new" {
		t.Errorf("Set overwrite: expected oci://new, got %v", spec["url"])
	}
}

func TestSet_SingleSegment(t *testing.T) {
	obj := map[string]interface{}{}
	Set(obj, "kind", "OCIRepository")
	if obj["kind"] != "OCIRepository" {
		t.Errorf("Set single segment: expected OCIRepository, got %v", obj["kind"])
	}
}

func TestGet_NonStringValuesSkipped(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"replicas": 3,
		},
	}
	got := Get(obj, "spec/replicas")
	// replicas is an int, not a string — Get should return empty
	if len(got) != 0 {
		t.Errorf("Get non-string: expected empty, got %v", got)
	}
}
