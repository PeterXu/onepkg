# json-update Subcommand Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `json-update` subcommand to onepkg that modifies key/value pairs in JSON files with indentation preservation, reusing `ParseValue()` from `toml_update.go`.

**Architecture:** Parse args in `main()`, detect `json-update` as first positional arg, delegate to `runJsonUpdate()`. Re-serialize JSON via `encoding/json` (stdlib) — no comments to preserve, so re-serialization is simpler and less fragile than text replacement. Navigate dotted key paths through `map[string]interface{}` and `[]interface{}` trees.

**Tech Stack:** Go 1.26, stdlib `encoding/json` only, no new dependencies.

---

### Task 1: Add `null` support to ParseValue

`ParseValue()` in `toml_update.go` currently handles bool, float64, int64, string. JSON needs `null` support. Add it as the first check (before bool).

**Files:**
- Modify: `cmd/onepkg/toml_update.go:15-31` (add `null` case to `ParseValue`)
- Modify: `cmd/onepkg/toml_update_test.go:11-38` (add `null` test case)

- [ ] **Step 1: Write the failing test**

Add a `null` test case to the existing table-driven test in `cmd/onepkg/toml_update_test.go`:

```go
{"null", nil},
```

Insert this line after the `{"false", false},` line (line 13), so it becomes:

```go
{"true", true},
{"false", false},
{"null", nil},
{"42", int64(42)},
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestParseValue -v`
Expected: FAIL — `ParseValue("null")` returns `"null"` (string), not `nil`

- [ ] **Step 3: Implement the `null` case**

In `cmd/onepkg/toml_update.go`, add a `null` check at the top of `ParseValue`, before the `true` check:

```go
func ParseValue(s string) interface{} {
	if s == "null" {
		return nil
	}
	if s == "true" {
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run TestParseValue -v`
Expected: PASS

- [ ] **Step 5: Run full test suite to verify no regressions**

Run: `cd cmd/onepkg && go test ./... -v`
Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/onepkg/toml_update.go cmd/onepkg/toml_update_test.go
git commit -m "feat: add null support to ParseValue for json-update"
```

---

### Task 2: navigateJSON — dotted key path navigation

Walk a `map[string]interface{}` / `[]interface{}` tree following a dotted key path. Returns `(parent, leafKey, found)` so the caller can read or write the target.

**Files:**
- Create: `cmd/onepkg/json_update.go`
- Create: `cmd/onepkg/json_update_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cmd/onepkg/json_update_test.go`:

```go
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
	idx := 0
	if leaf != "0" {
		t.Errorf("leaf = %q, want %q", leaf, "0")
	}
	if arr[idx] != "old" {
		t.Errorf("value = %v, want old", arr[idx])
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test -run TestNavigateJSON -v`
Expected: FAIL — `navigateJSON` undefined

- [ ] **Step 3: Implement navigateJSON**

Create `cmd/onepkg/json_update.go`:

```go
package main

import (
	"strconv"
)

// navigateJSON walks a JSON tree following a dotted key path.
// Returns (parent, leafKey, found).
// parent is the map or slice containing the target.
// leafKey is the final key string (for maps) or index string (for slices).
func navigateJSON(data interface{}, keyPath string) (interface{}, string, bool) {
	parts := splitKeyPath(keyPath)
	if len(parts) == 0 {
		return nil, "", false
	}

	current := data
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		switch v := current.(type) {
		case map[string]interface{}:
			next, ok := v[part]
			if !ok {
				return nil, "", false
			}
			current = next
		case []interface{}:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(v) {
				return nil, "", false
			}
			current = v[idx]
		default:
			return nil, "", false
		}
	}

	leaf := parts[len(parts)-1]

	// Return parent and leaf; found depends on whether leaf exists
	switch v := current.(type) {
	case map[string]interface{}:
		_, exists := v[leaf]
		return v, leaf, exists
	case []interface{}:
		idx, err := strconv.Atoi(leaf)
		if err != nil || idx < 0 || idx >= len(v) {
			return nil, "", false
		}
		return v, leaf, true
	default:
		return nil, "", false
	}
}

// splitKeyPath splits a dotted key path into parts.
func splitKeyPath(key string) []string {
	if key == "" {
		return nil
	}
	return strings.Split(key, ".")
}
```

Add the missing `strings` import — the file needs:

```go
import (
	"strconv"
	"strings"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test -run TestNavigateJSON -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/json_update.go cmd/onepkg/json_update_test.go
git commit -m "feat(json-update): add navigateJSON for dotted key path walking"
```

---

### Task 3: detectIndentation — preserve original JSON formatting

Detect the indentation style from raw JSON bytes so re-serialized output matches the original.

**Files:**
- Modify: `cmd/onepkg/json_update.go`
- Modify: `cmd/onepkg/json_update_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onepkg/json_update_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test -run TestDetectIndentation -v`
Expected: FAIL — `detectIndentation` undefined

- [ ] **Step 3: Implement detectIndentation**

Append to `cmd/onepkg/json_update.go`:

```go
import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

// detectIndentation examines raw JSON bytes to determine indentation style.
// Returns the indentation string (e.g., "  ", "    ", "\t").
// Defaults to 2-space if no indentation is detected.
func detectIndentation(raw []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}
		if trimmed == "{" || trimmed == "[" || trimmed == "}" || trimmed == "]" {
			continue
		}
		// Found a content line — extract leading whitespace
		indent := line[:len(line)-len(trimmed)]
		if indent != "" {
			return indent
		}
	}
	return "  " // default
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test -run TestDetectIndentation -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/json_update.go cmd/onepkg/json_update_test.go
git commit -m "feat(json-update): add detectIndentation for format preservation"
```

---

### Task 4: formatJSONValue — Go value to JSON literal

Convert a Go value (`nil`, `bool`, `int64`, `float64`, `string`) to its JSON representation string.

**Files:**
- Modify: `cmd/onepkg/json_update.go`
- Modify: `cmd/onepkg/json_update_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onepkg/json_update_test.go`:

```go
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
```

Add `"fmt"` to the test file's imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test -run TestFormatJSONValue -v`
Expected: FAIL — `formatJSONValue` undefined

- [ ] **Step 3: Implement formatJSONValue**

Append to `cmd/onepkg/json_update.go`:

```go
import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// formatJSONValue converts a Go value to its JSON literal string representation.
func formatJSONValue(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return "null"
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case string:
		escaped := strings.ReplaceAll(val, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		escaped = strings.ReplaceAll(escaped, "\n", `\n`)
		escaped = strings.ReplaceAll(escaped, "\t", `\t`)
		escaped = strings.ReplaceAll(escaped, "\r", `\r`)
		return `"` + escaped + `"`
	default:
		return fmt.Sprintf("%v", val)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test -run TestFormatJSONValue -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/json_update.go cmd/onepkg/json_update_test.go
git commit -m "feat(json-update): add formatJSONValue for Go-to-JSON conversion"
```

---

### Task 5: jsonUpdateCore — main orchestration function

Wire together: parse file, navigate, check existence, type-preserve, set value, re-serialize, write.

**Files:**
- Modify: `cmd/onepkg/json_update.go`
- Modify: `cmd/onepkg/json_update_test.go`

- [ ] **Step 1: Write the failing tests for core update logic**

Append to `cmd/onepkg/json_update_test.go`:

```go
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
```

Add `"os"` and `"strings"` to the test file's imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test -run TestRunJsonUpdate -v`
Expected: FAIL — `runJsonUpdate` undefined

- [ ] **Step 3: Implement runJsonUpdate and jsonUpdateCore**

Append to `cmd/onepkg/json_update.go`. Add `"encoding/json"` and `"os"` to imports:

```go
import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// runJsonUpdate executes the json-update subcommand (update-only).
func runJsonUpdate(args []string) error {
	return jsonUpdateCore(args, false)
}

// runJsonUpdateAdd executes the json-update subcommand with --add (upsert).
func runJsonUpdateAdd(args []string) error {
	return jsonUpdateCore(args, true)
}

func jsonUpdateCore(args []string, allowAdd bool) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: onepkg json-update [--add] <key> <value> <file>")
	}

	key := args[0]
	filePath := args[2]

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", filePath, err)
	}

	var data interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("parse %s: %w", filePath, err)
	}

	root, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("root is not a JSON object")
	}

	parent, leaf, found := navigateJSON(root, key)

	if !found && !allowAdd {
		return fmt.Errorf("key %q does not exist (use --add to insert)", key)
	}

	newVal := ParseValue(args[1])

	if found {
		// Get old value for type preservation and no-op check
		var oldVal interface{}
		switch v := parent.(type) {
		case map[string]interface{}:
			oldVal = v[leaf]
		case []interface{}:
			idx, _ := strconv.Atoi(leaf)
			oldVal = v[idx]
		}

		// Type preservation: if old value is a string, treat new value as string
		if _, isStr := oldVal.(string); isStr {
			newVal = args[1]
		}

		// No-op check
		oldStr := formatJSONValue(oldVal)
		newStr := formatJSONValue(newVal)
		if oldStr == newStr {
			return nil
		}

		// Set new value
		switch v := parent.(type) {
		case map[string]interface{}:
			v[leaf] = newVal
		case []interface{}:
			idx, _ := strconv.Atoi(leaf)
			v[idx] = newVal
		}
	} else {
		// --add mode: key not found, insert new
		// Only support adding to map (not array append)
		m, ok := parent.(map[string]interface{})
		if !ok {
			return fmt.Errorf("cannot add key %q to non-object parent", key)
		}
		m[leaf] = newVal
	}

	// Re-serialize with original indentation
	indent := detectIndentation(raw)
	result, err := json.MarshalIndent(root, "", indent)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	result = append(result, '\n')

	return os.WriteFile(filePath, result, 0644)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test -run TestRunJsonUpdate -v`
Expected: PASS

- [ ] **Step 5: Run all tests**

Run: `cd cmd/onepkg && go test ./... -v`
Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/onepkg/json_update.go cmd/onepkg/json_update_test.go
git commit -m "feat(json-update): add jsonUpdateCore with update, upsert, type preservation"
```

---

### Task 6: --add mode tests

Test the upsert behavior: insert if key missing, update if exists.

**Files:**
- Modify: `cmd/onepkg/json_update_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onepkg/json_update_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test -run TestRunJsonUpdateAdd -v`
Expected: PASS — `runJsonUpdateAdd` was implemented in Task 5

- [ ] **Step 3: Commit**

```bash
git add cmd/onepkg/json_update_test.go
git commit -m "test(json-update): add --add mode upsert tests"
```

---

### Task 7: Format preservation tests

Verify that indentation style is preserved after updates.

**Files:**
- Modify: `cmd/onepkg/json_update_test.go`

- [ ] **Step 1: Write the tests**

Append to `cmd/onepkg/json_update_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test -run TestRunJsonUpdate_Preserves -v`
Expected: PASS — format preservation is already implemented via `detectIndentation`

- [ ] **Step 3: Commit**

```bash
git add cmd/onepkg/json_update_test.go
git commit -m "test(json-update): add format preservation tests for indentation"
```

---

### Task 8: Wire json-update into main.go

Add the `json-update` subcommand dispatch in `main()` and update `printHelp()`.

**Files:**
- Modify: `cmd/onepkg/main.go`

- [ ] **Step 1: Add dispatch block**

In `cmd/onepkg/main.go`, after the `toml-update` dispatch block (after line 39, before line 41), add:

```go
	if os.Args[1] == "json-update" {
		var addMode bool
		rest := os.Args[2:]
		if len(rest) > 0 && rest[0] == "--add" {
			addMode = true
			rest = rest[1:]
		}
		var err error
		if addMode {
			err = runJsonUpdateAdd(rest)
		} else {
			err = runJsonUpdate(rest)
		}
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		return
	}
```

- [ ] **Step 2: Update printHelp()**

In the `printHelp()` function, update the usage and subcommand sections. Replace the current help string (lines 283-317) with:

```go
func printHelp() {
	fmt.Println(`onepkg - General-purpose self-extracting installer generator

Usage:
  onepkg -c config.toml --platform <platforms> [options]
  onepkg toml-update [--add] <key> <value> <file>
  onepkg json-update [--add] <key> <value> <file>

Subcommands:
  toml-update           Update a key/value in a TOML file
    --add               Upsert: add if key doesn't exist, update if it does
  json-update           Update a key/value in a JSON file
    --add               Upsert: add if key doesn't exist, update if it does

Options:
  -c, --config <path>      Path to config.toml (required)
      --platform <p>       Target platforms, comma-separated (required)
                           e.g., --platform linux-arm64
                           e.g., --platform linux-arm64,linux-amd64
      --output <dir>       Override output directory
  -h, --help               Show this help

Supported platforms:
  linux-amd64, linux-arm64
  darwin-amd64, darwin-arm64

Item syntax:
  Items can use {platform} marker for platform-specific files:
    "bin/myapp.{platform}"  -> resolves to bin/myapp.linux-arm64, packed as bin/myapp
  Literal items are unchanged:
    "etc/config.conf"       -> packed as etc/config.conf

Examples:
  onepkg -c config.toml --platform linux-arm64
  onepkg -c config.toml --platform linux-arm64,linux-amd64
  onepkg -c config.toml --platform darwin-arm64 --output ./dist
  onepkg toml-update version "1.0.1" config.toml
  onepkg toml-update --add newkey "value" config.toml
  onepkg json-update version "1.0.1" package.json
  onepkg json-update --add newField "value" package.json`)
}
```

- [ ] **Step 3: Build and verify compilation**

Run: `cd cmd/onepkg && go build -o onepkg .`
Expected: Build succeeds with no errors

- [ ] **Step 4: Test help output**

Run: `cd cmd/onepkg && ./onepkg --help`
Expected: Help text includes `json-update` subcommand

- [ ] **Step 5: Test basic json-update invocation**

```bash
cd cmd/onepkg
echo '{"name": "old"}' > /tmp/test.json
./onepkg json-update name "new" /tmp/test.json
cat /tmp/test.json
```
Expected: `{"name": "new"}` (with indentation)

- [ ] **Step 6: Run all tests**

Run: `cd cmd/onepkg && go test ./... -v`
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/onepkg/main.go
git commit -m "feat(json-update): wire json-update subcommand into CLI dispatch"
```

---

### Task 9: Edge case tests

Cover remaining edge cases from the design spec.

**Files:**
- Modify: `cmd/onepkg/json_update_test.go`

- [ ] **Step 1: Write edge case tests**

Append to `cmd/onepkg/json_update_test.go`:

```go
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

	// Verify the result is valid JSON
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

func TestRunJsonUpdate_RootIsArray(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.json"
	os.WriteFile(path, []byte(`["a", "b", "c"]`), 0644)

	err := runJsonUpdate([]string{"0", "x", path})
	if err == nil {
		t.Error("expected error for non-object root")
	}
}
```

Add `"encoding/json"` to the test file's imports.

- [ ] **Step 2: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test -run 'TestRunJsonUpdate_(Empty|Deep|Value|ArrayPrimitive|Root)' -v`
Expected: PASS — all edge cases handled by existing implementation

- [ ] **Step 3: Run full test suite**

Run: `cd cmd/onepkg && go test ./... -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/onepkg/json_update_test.go
git commit -m "test(json-update): add edge case tests for deep nesting, special chars, arrays"
```
