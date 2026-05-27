package main

import (
	"testing"
)

func TestNavigateJSON_TopLevel(t *testing.T) {
	data := map[string]interface{}{
		"name": "alice",
		"age":  float64(30),
	}
	parent, leaf, found := navigateJSON(data, "name")
	if !found {
		t.Fatal("expected name to be found")
	}
	m, ok := parent.(map[string]interface{})
	if !ok {
		t.Fatalf("parent type = %T, want map[string]interface{}", parent)
	}
	if leaf != "name" {
		t.Errorf("leaf = %q, want %q", leaf, "name")
	}
	if m[leaf] != "alice" {
		t.Errorf("value = %v, want alice", m[leaf])
	}
}

func TestNavigateJSON_Nested(t *testing.T) {
	data := map[string]interface{}{
		"services": map[string]interface{}{
			"web": map[string]interface{}{
				"port": float64(8080),
			},
		},
	}
	parent, leaf, found := navigateJSON(data, "services.web.port")
	if !found {
		t.Fatal("expected services.web.port to be found")
	}
	m, ok := parent.(map[string]interface{})
	if !ok {
		t.Fatalf("parent type = %T, want map[string]interface{}", parent)
	}
	if leaf != "port" {
		t.Errorf("leaf = %q, want %q", leaf, "port")
	}
	if m[leaf] != float64(8080) {
		t.Errorf("value = %v, want 8080", m[leaf])
	}
}

func TestNavigateJSON_ArrayField(t *testing.T) {
	data := map[string]interface{}{
		"dirs": []interface{}{
			map[string]interface{}{"path": "/var/log"},
			map[string]interface{}{"path": "/var/lib"},
		},
	}
	parent, leaf, found := navigateJSON(data, "dirs.0.path")
	if !found {
		t.Fatal("expected dirs.0.path to be found")
	}
	m, ok := parent.(map[string]interface{})
	if !ok {
		t.Fatalf("parent type = %T, want map[string]interface{}", parent)
	}
	if leaf != "path" {
		t.Errorf("leaf = %q, want %q", leaf, "path")
	}
	if m[leaf] != "/var/log" {
		t.Errorf("value = %v, want /var/log", m[leaf])
	}
}

func TestNavigateJSON_ArrayPrimitive(t *testing.T) {
	data := map[string]interface{}{
		"tags": []interface{}{"old", "stale"},
	}
	parent, leaf, found := navigateJSON(data, "tags.0")
	if !found {
		t.Fatal("expected tags.0 to be found")
	}
	arr, ok := parent.([]interface{})
	if !ok {
		t.Fatalf("parent type = %T, want []interface{}", parent)
	}
	if leaf != "0" {
		t.Errorf("leaf = %q, want %q", leaf, "0")
	}
	if arr[0] != "old" {
		t.Errorf("value = %v, want old", arr[0])
	}
}

func TestNavigateJSON_KeyNotFound(t *testing.T) {
	data := map[string]interface{}{
		"name": "alice",
	}
	_, _, found := navigateJSON(data, "missing")
	if found {
		t.Error("expected missing key to not be found")
	}
}

func TestNavigateJSON_ArrayIndexOutOfRange(t *testing.T) {
	data := map[string]interface{}{
		"items": []interface{}{"one"},
	}
	_, _, found := navigateJSON(data, "items.5")
	if found {
		t.Error("expected out-of-range index to not be found")
	}
}

func TestNavigateJSON_IntermediateNotFound(t *testing.T) {
	data := map[string]interface{}{
		"name": "alice",
	}
	_, _, found := navigateJSON(data, "a.b.c")
	if found {
		t.Error("expected intermediate path to not be found")
	}
}

func TestNavigateJSON_ArraySecondElement(t *testing.T) {
	data := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"name": "first"},
			map[string]interface{}{"name": "second"},
		},
	}
	parent, leaf, found := navigateJSON(data, "items.1.name")
	if !found {
		t.Fatal("expected items.1.name to be found")
	}
	m, ok := parent.(map[string]interface{})
	if !ok {
		t.Fatalf("parent type = %T, want map[string]interface{}", parent)
	}
	if m[leaf] != "second" {
		t.Errorf("value = %v, want second", m[leaf])
	}
}

func TestDetectIndentation_TwoSpaces(t *testing.T) {
	raw := []byte("{\n  \"name\": \"alice\"\n}")
	indent := detectIndentation(raw)
	if indent != "  " {
		t.Errorf("got %q, want %q", indent, "  ")
	}
}

func TestDetectIndentation_FourSpaces(t *testing.T) {
	raw := []byte("{\n    \"name\": \"alice\"\n}")
	indent := detectIndentation(raw)
	if indent != "    " {
		t.Errorf("got %q, want %q", indent, "    ")
	}
}

func TestDetectIndentation_Tab(t *testing.T) {
	raw := []byte("{\n\t\"name\": \"alice\"\n}")
	indent := detectIndentation(raw)
	if indent != "\t" {
		t.Errorf("got %q, want tab", indent)
	}
}

func TestDetectIndentation_Default(t *testing.T) {
	raw := []byte("{\"name\": \"alice\"}")
	indent := detectIndentation(raw)
	if indent != "  " {
		t.Errorf("got %q, want default %q", indent, "  ")
	}
}

func TestDetectIndentation_Array(t *testing.T) {
	raw := []byte("[\n  \"a\",\n  \"b\"\n]")
	indent := detectIndentation(raw)
	if indent != "  " {
		t.Errorf("got %q, want %q", indent, "  ")
	}
}
