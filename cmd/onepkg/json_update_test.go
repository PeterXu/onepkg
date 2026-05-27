package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
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

func TestFormatJSONValue(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected string
	}{
		{nil, "null"},
		{true, "true"},
		{false, "false"},
		{int64(42), "42"},
		{int64(-1), "-1"},
		{float64(3.14), "3.14"},
		{"hello", `"hello"`},
		{`say "hi"`, `"say \"hi\""`},
		{"line1\nline2", `"line1\nline2"`},
		{"tab\there", `"tab\there"`},
		{`back\slash`, `"back\\slash"`},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v", tt.input), func(t *testing.T) {
			result := formatJSONValue(tt.input)
			if result != tt.expected {
				t.Errorf("formatJSONValue(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRunJsonUpdate_TopLevelString(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"name": "old", "version": "v1.0.0"}`), 0644)

	err := runJsonUpdate([]string{"name", "new", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := `"name": "new"`
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
	if !strings.Contains(string(data), `"version": "v1.0.0"`) {
		t.Errorf("version should be preserved, got: %q", string(data))
	}
}

func TestRunJsonUpdate_NestedKey(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"services": {"web": {"port": 3000}}}`), 0644)

	err := runJsonUpdate([]string{"services.web.port", "8080", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := `"port": 8080`
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
}

func TestRunJsonUpdate_ArrayElement(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"dirs": [{"path": "/var/log"}, {"path": "/var/lib"}]}`), 0644)

	err := runJsonUpdate([]string{"dirs.0.path", "/tmp/log", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := `"path": "/tmp/log"`
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
	if !strings.Contains(string(data), `"/var/lib"`) {
		t.Errorf("dirs.1.path should be preserved, got: %q", string(data))
	}
}

func TestRunJsonUpdate_BoolValue(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"enabled": false}`), 0644)

	err := runJsonUpdate([]string{"enabled", "true", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := `"enabled": true`
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
}

func TestRunJsonUpdate_IntValue(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"count": 5}`), 0644)

	err := runJsonUpdate([]string{"count", "10", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := `"count": 10`
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
}

func TestRunJsonUpdate_FloatValue(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"rate": 1.0}`), 0644)

	err := runJsonUpdate([]string{"rate", "3.14", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := `"rate": 3.14`
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
}

func TestRunJsonUpdate_NullValue(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"deprecated": "yes"}`), 0644)

	err := runJsonUpdate([]string{"deprecated", "null", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := `"deprecated": null`
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
}

func TestRunJsonUpdate_StringTypePreserved(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"name": "true"}`), 0644)

	err := runJsonUpdate([]string{"name", "false", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := `"name": "false"`
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q (string, not bool)", string(data), want)
	}
}

func TestRunJsonUpdate_KeyNotExist(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"name": "old"}`), 0644)

	err := runJsonUpdate([]string{"missing", "val", path})
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %q, want 'does not exist'", err.Error())
	}
}

func TestRunJsonUpdate_FileNotExist(t *testing.T) {
	err := runJsonUpdate([]string{"name", "val", "/nonexistent.json"})
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestRunJsonUpdate_TooFewArgs(t *testing.T) {
	err := runJsonUpdate([]string{"name", "val"})
	if err == nil {
		t.Error("expected error for too few args")
	}
}

func TestRunJsonUpdate_SameValue(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"name": "old"}`), 0644)

	original, _ := os.ReadFile(path)
	err := runJsonUpdate([]string{"name", "old", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	after, _ := os.ReadFile(path)
	if string(original) != string(after) {
		t.Errorf("file should not change when value is the same\nbefore: %q\nafter: %q", string(original), string(after))
	}
}

func TestRunJsonUpdate_ArrayIndexOutOfRange(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"items": ["one"]}`), 0644)

	err := runJsonUpdate([]string{"items.5", "x", path})
	if err == nil {
		t.Error("expected error for out-of-range index")
	}
}

func TestRunJsonUpdate_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{invalid`), 0644)

	err := runJsonUpdate([]string{"name", "val", path})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestRunJsonUpdateAdd_NewKey(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"name": "old"}`), 0644)

	err := runJsonUpdateAdd([]string{"newkey", "newval", path})
	if err != nil {
		t.Fatalf("runJsonUpdateAdd: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, `"newkey": "newval"`) {
		t.Errorf("new key not added, got: %s", s)
	}
	if !strings.Contains(s, `"name": "old"`) {
		t.Errorf("original key should be preserved, got: %s", s)
	}
}

func TestRunJsonUpdateAdd_ExistingKey(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"name": "old"}`), 0644)

	err := runJsonUpdateAdd([]string{"name", "updated", path})
	if err != nil {
		t.Fatalf("runJsonUpdateAdd: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := `"name": "updated"`
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
}

func TestRunJsonUpdateAdd_NestedNewKey(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"services": {"web": {"port": 80}}}`), 0644)

	err := runJsonUpdateAdd([]string{"services.web.host", "localhost", path})
	if err != nil {
		t.Fatalf("runJsonUpdateAdd: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, `"host": "localhost"`) {
		t.Errorf("new nested key not added, got: %s", s)
	}
	if !strings.Contains(s, `"port": 80`) {
		t.Errorf("existing key should be preserved, got: %s", s)
	}
}

func TestRunJsonUpdate_PreservesTwoSpaceIndent(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	input := "{\n  \"name\": \"old\",\n  \"version\": \"v1\"\n}\n"
	os.WriteFile(path, []byte(input), 0644)

	err := runJsonUpdate([]string{"name", "new", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "  \"name\": \"new\"") {
		t.Errorf("2-space indent not preserved, got: %s", s)
	}
}

func TestRunJsonUpdate_PreservesFourSpaceIndent(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	input := "{\n    \"name\": \"old\"\n}\n"
	os.WriteFile(path, []byte(input), 0644)

	err := runJsonUpdate([]string{"name", "new", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "    \"name\": \"new\"") {
		t.Errorf("4-space indent not preserved, got: %s", s)
	}
}

func TestRunJsonUpdate_PreservesTabIndent(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	input := "{\n\t\"name\": \"old\"\n}\n"
	os.WriteFile(path, []byte(input), 0644)

	err := runJsonUpdate([]string{"name", "new", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "\t\"name\": \"new\"") {
		t.Errorf("tab indent not preserved, got: %s", s)
	}
}

func TestRunJsonUpdate_EmptyObject(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{}`), 0644)

	err := runJsonUpdateAdd([]string{"name", "alice", path})
	if err != nil {
		t.Fatalf("runJsonUpdateAdd on empty object: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := `"name": "alice"`
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
}

func TestRunJsonUpdate_DeepNesting(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"a": {"b": {"c": {"d": {"e": {"f": "deep"}}}}}}`), 0644)

	err := runJsonUpdate([]string{"a.b.c.d.e.f", "updated", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := `"f": "updated"`
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
}

func TestRunJsonUpdate_ValueWithQuotes(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"msg": "hello"}`), 0644)

	err := runJsonUpdate([]string{"msg", `say "hi"`, path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("result is not valid JSON: %v\ncontent: %s", err, string(data))
	}
	if result["msg"] != `say "hi"` {
		t.Errorf("msg = %v, want %q", result["msg"], `say "hi"`)
	}
}

func TestRunJsonUpdate_ValueWithBackslash(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"path": "/usr/bin"}`), 0644)

	err := runJsonUpdate([]string{"path", `C:\Users\test`, path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("result is not valid JSON: %v\ncontent: %s", err, string(data))
	}
	if result["path"] != `C:\Users\test` {
		t.Errorf("path = %v, want %q", result["path"], `C:\Users\test`)
	}
}

func TestRunJsonUpdate_ArrayPrimitive(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"tags": ["old", "stale"]}`), 0644)

	err := runJsonUpdate([]string{"tags.0", "new", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := `"new"`
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
	if !strings.Contains(string(data), `"stale"`) {
		t.Errorf("tags.1 should be preserved, got: %q", string(data))
	}
}

func TestRunJsonUpdate_PreservesFieldOrder(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	// Deliberately non-alphabetical order
	os.WriteFile(path, []byte(`{"zebra": 1, "alpha": 2, "middle": 3}`), 0644)

	err := runJsonUpdate([]string{"middle", "99", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)

	zebraIdx := strings.Index(s, `"zebra"`)
	alphaIdx := strings.Index(s, `"alpha"`)
	middleIdx := strings.Index(s, `"middle"`)

	if zebraIdx == -1 || alphaIdx == -1 || middleIdx == -1 {
		t.Fatalf("missing fields in output: %s", s)
	}
	if !(zebraIdx < alphaIdx && alphaIdx < middleIdx) {
		t.Errorf("field order not preserved, expected zebra < alpha < middle, got: %s", s)
	}
}

func TestRunJsonUpdate_PreservesNestedFieldOrder(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	input := `{"zebra": {"beta": 1, "alpha": 2}, "alpha": 3}`
	os.WriteFile(path, []byte(input), 0644)

	err := runJsonUpdate([]string{"zebra.alpha", "99", path})
	if err != nil {
		t.Fatalf("runJsonUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)

	// Root order: zebra before alpha
	zebraIdx := strings.Index(s, `"zebra"`)
	alphaRootIdx := strings.Index(s, `"alpha": 3`)
	if zebraIdx == -1 || alphaRootIdx == -1 {
		t.Fatalf("missing root fields in output: %s", s)
	}
	if !(zebraIdx < alphaRootIdx) {
		t.Errorf("root field order not preserved, expected zebra < alpha, got: %s", s)
	}

	// Nested order: beta before alpha inside zebra
	betaIdx := strings.Index(s, `"beta"`)
	alphaNestedIdx := strings.Index(s, `"alpha": 99`)
	if betaIdx == -1 || alphaNestedIdx == -1 {
		t.Fatalf("missing nested fields in output: %s", s)
	}
	if !(betaIdx < alphaNestedIdx) {
		t.Errorf("nested field order not preserved, expected beta < alpha, got: %s", s)
	}
}

func TestRunJsonUpdateAdd_PreservesFieldOrder_AppendsNewKey(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`{"zebra": 1, "alpha": 2}`), 0644)

	err := runJsonUpdateAdd([]string{"newkey", "3", path})
	if err != nil {
		t.Fatalf("runJsonUpdateAdd: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)

	zebraIdx := strings.Index(s, `"zebra"`)
	alphaIdx := strings.Index(s, `"alpha"`)
	newIdx := strings.Index(s, `"newkey"`)

	if zebraIdx == -1 || alphaIdx == -1 || newIdx == -1 {
		t.Fatalf("missing fields in output: %s", s)
	}
	// Original order preserved, new key appended at end
	if !(zebraIdx < alphaIdx && alphaIdx < newIdx) {
		t.Errorf("field order not preserved, expected zebra < alpha < newkey, got: %s", s)
	}
}

func TestRunJsonUpdate_RootIsArray(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`["a", "b", "c"]`), 0644)

	err := runJsonUpdate([]string{"0", "x", path})
	if err == nil {
		t.Error("expected error for non-object root")
	}
}
