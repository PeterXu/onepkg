# toml-update Subcommand Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `toml-update` subcommand to onepkg that modifies key/value pairs in TOML files with format preservation.

**Architecture:** Parse args in `main()`, detect `toml-update` as first positional arg, delegate to `runTomlUpdate()`. Use BurntSushi's `toml.Tree` API for format-preserving reads/writes. Value auto-detection converts string input to bool/float64/int64/string.

**Tech Stack:** Go 1.26, BurntSushi/toml v1.6.0 (already vendored), no new dependencies.

---

### Task 1: Value auto-detection function

**Files:**
- Create: `cmd/onepkg/toml_update.go`
- Test: `cmd/onepkg/toml_update_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"testing"
)

func TestParseValue(t *testing.T) {
	tests := []struct {
		input    string
		expected interface{}
	}{
		{"true", true},
		{"false", false},
		{"42", int64(42)},
		{"0", int64(0)},
		{"-1", int64(-1)},
		{"3.14", float64(3.14)},
		{"0.5", float64(0.5)},
		{"-2.7", float64(-2.7)},
		{"hello", "hello"},
		{"true story", "true story"},
		{"", ""},
		{"123abc", "123abc"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseValue(tt.input)
			if result != tt.expected {
				t.Errorf("ParseValue(%q) = %v (%T), want %v (%T)", tt.input, result, result, tt.expected, tt.expected)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestParseValue -v`
Expected: FAIL — `ParseValue` undefined

- [ ] **Step 3: Write minimal implementation**

```go
package main

import (
	"strconv"
	"strings"
)

// ParseValue converts a string input to the appropriate Go type:
// bool → float64 → int64 → string (fallback)
func ParseValue(s string) interface{} {
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	if strings.Contains(s, ".") {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run TestParseValue -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/toml_update.go cmd/onepkg/toml_update_test.go
git commit -m "feat(toml-update): add ParseValue for auto-detecting TOML value types"
```

---

### Task 2: Core `runTomlUpdate` function (update-only, no --add)

**Files:**
- Modify: `cmd/onepkg/toml_update.go`
- Test: `cmd/onepkg/toml_update_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onepkg/toml_update_test.go`:

```go
func TestRunTomlUpdate_TopLevelString(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	os.WriteFile(path, []byte("name = \"old\"\nversion = \"v1.0.0\"\n"), 0644)

	err := runTomlUpdate([]string{"name", "new", path})
	if err != nil {
		t.Fatalf("runTomlUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := "name = \"new\""
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
	if !strings.Contains(string(data), "version = \"v1.0.0\"") {
		t.Errorf("version should be preserved, got: %q", string(data))
	}
}

func TestRunTomlUpdate_BoolValue(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	os.WriteFile(path, []byte("enabled = false\n"), 0644)

	err := runTomlUpdate([]string{"enabled", "true", path})
	if err != nil {
		t.Fatalf("runTomlUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := "enabled = true"
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
}

func TestRunTomlUpdate_IntValue(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	os.WriteFile(path, []byte("count = 5\n"), 0644)

	err := runTomlUpdate([]string{"count", "10", path})
	if err != nil {
		t.Fatalf("runTomlUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := "count = 10"
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
}

func TestRunTomlUpdate_NestedKey(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	os.WriteFile(path, []byte("[services]\nname = \"old\"\n"), 0644)

	err := runTomlUpdate([]string{"services.name", "new", path})
	if err != nil {
		t.Fatalf("runTomlUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := "name = \"new\""
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
}

func TestRunTomlUpdate_KeyNotExist(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	os.WriteFile(path, []byte("name = \"old\"\n"), 0644)

	err := runTomlUpdate([]string{"nonexistent", "val", path})
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %q, want 'does not exist'", err.Error())
	}
}

func TestRunTomlUpdate_FileNotExist(t *testing.T) {
	err := runTomlUpdate([]string{"name", "val", "/nonexistent.toml"})
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestRunTomlUpdate_TooFewArgs(t *testing.T) {
	err := runTomlUpdate([]string{"name", "val"})
	if err == nil {
		t.Error("expected error for too few args")
	}
}
```

Add `"os"` and `"strings"` to test imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test -run TestRunTomlUpdate -v`
Expected: FAIL — `runTomlUpdate` undefined

- [ ] **Step 3: Write implementation**

Append to `cmd/onepkg/toml_update.go`:

```go
import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// runTomlUpdate executes the toml-update subcommand.
// args = [key, value, file] with optional "--add" already stripped by caller.
func runTomlUpdate(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: onepkg toml-update [--add] <key> <value> <file>")
	}

	key := args[0]
	value := ParseValue(args[1])
	filePath := args[2]

	tree, err := toml.LoadFile(filePath)
	if err != nil {
		return fmt.Errorf("load %s: %w", filePath, err)
	}

	if !tree.Has(key) {
		return fmt.Errorf("key %q does not exist (use --add to insert)", key)
	}

	tree.Set(key, value)

	var buf strings.Builder
	if _, err := tree.WriteTo(&buf); err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(buf.String()), 0644); err != nil {
		return fmt.Errorf("write %s: %w", filePath, err)
	}

	return nil
}
```

Add `"strings"` to the imports in `toml_update.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test -run TestRunTomlUpdate -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/toml_update.go cmd/onepkg/toml_update_test.go
git commit -m "feat(toml-update): add runTomlUpdate with update-only mode"
```

---

### Task 3: `--add` flag support (upsert)

**Files:**
- Modify: `cmd/onepkg/toml_update.go`
- Test: `cmd/onepkg/toml_update_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onepkg/toml_update_test.go`:

```go
func TestRunTomlUpdate_Add_NewKey(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	os.WriteFile(path, []byte("name = \"old\"\n"), 0644)

	err := runTomlUpdateAdd([]string{"newkey", "newval", path})
	if err != nil {
		t.Fatalf("runTomlUpdateAdd: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := `newkey = "newval"`
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
	if !strings.Contains(string(data), `name = "old"`) {
		t.Errorf("original key should be preserved, got: %q", string(data))
	}
}

func TestRunTomlUpdate_Add_ExistingKey(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	os.WriteFile(path, []byte("name = \"old\"\n"), 0644)

	err := runTomlUpdateAdd([]string{"name", "updated", path})
	if err != nil {
		t.Fatalf("runTomlUpdateAdd: %v", err)
	}

	data, _ := os.ReadFile(path)
	want := `name = "updated"`
	if !strings.Contains(string(data), want) {
		t.Errorf("file content = %q, want to contain %q", string(data), want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test -run TestRunTomlUpdate_Add -v`
Expected: FAIL — `runTomlUpdateAdd` undefined

- [ ] **Step 3: Write implementation**

Append to `cmd/onepkg/toml_update.go`:

```go
// runTomlUpdateAdd executes the toml-update subcommand with --add (upsert).
func runTomlUpdateAdd(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: onepkg toml-update --add <key> <value> <file>")
	}

	key := args[0]
	value := ParseValue(args[1])
	filePath := args[2]

	tree, err := toml.LoadFile(filePath)
	if err != nil {
		return fmt.Errorf("load %s: %w", filePath, err)
	}

	tree.Set(key, value)

	var buf strings.Builder
	if _, err := tree.WriteTo(&buf); err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(buf.String()), 0644); err != nil {
		return fmt.Errorf("write %s: %w", filePath, err)
	}

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test -run TestRunTomlUpdate_Add -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/toml_update.go cmd/onepkg/toml_update_test.go
git commit -m "feat(toml-update): add --add upsert mode"
```

---

### Task 4: Array index support via dotted path

**Files:**
- Modify: `cmd/onepkg/toml_update.go`
- Test: `cmd/onepkg/toml_update_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onepkg/toml_update_test.go`:

```go
func TestRunTomlUpdate_ArrayElement(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := `name = "myapp"

[[dirs]]
path = "/var/log/myapp"
owner = "root"

[[dirs]]
path = "/var/lib/myapp"
owner = "app"
`
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdate([]string{"dirs.0.owner", "nobody", path})
	if err != nil {
		t.Fatalf("runTomlUpdate: %v", err)
	}

	tree, _ := toml.LoadFile(path)
	arr := tree.Get("dirs").([]*toml.Tree)
	if arr[0].Get("owner") != "nobody" {
		t.Errorf("dirs.0.owner = %v, want nobody", arr[0].Get("owner"))
	}
	if arr[1].Get("owner") != "app" {
		t.Errorf("dirs.1.owner should be unchanged, got %v", arr[1].Get("owner"))
	}
}

func TestRunTomlUpdate_ArrayElementIndexOutOfRange(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := "[[dirs]]\npath = \"/var/log\"\n"
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdate([]string{"dirs.5.path", "/x", path})
	if err == nil {
		t.Error("expected error for out-of-range index")
	}
}
```

Add `"github.com/BurntSushi/toml"` to test imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test -run "TestRunTomlUpdate_Array" -v`
Expected: FAIL — `tree.Set("dirs.0.owner", ...)` doesn't work with arrays natively

- [ ] **Step 3: Refactor implementation to handle array indices**

Replace `runTomlUpdate` and `runTomlUpdateAdd` in `cmd/onepkg/toml_update.go` with a shared core that handles array indices. The full file should contain:

```go
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// ParseValue converts a string input to the appropriate Go type:
// bool → float64 → int64 → string (fallback)
func ParseValue(s string) interface{} {
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	if strings.Contains(s, ".") {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	return s
}

// runTomlUpdate executes the toml-update subcommand (update-only).
func runTomlUpdate(args []string) error {
	return tomlUpdateCore(args, false)
}

// runTomlUpdateAdd executes the toml-update subcommand with --add (upsert).
func runTomlUpdateAdd(args []string) error {
	return tomlUpdateCore(args, true)
}

func tomlUpdateCore(args []string, allowAdd bool) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: onepkg toml-update [--add] <key> <value> <file>")
	}

	key := args[0]
	value := ParseValue(args[1])
	filePath := args[2]

	tree, err := toml.LoadFile(filePath)
	if err != nil {
		return fmt.Errorf("load %s: %w", filePath, err)
	}

	if err := setKey(tree, key, value, allowAdd); err != nil {
		return err
	}

	var buf strings.Builder
	if _, err := tree.WriteTo(&buf); err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(buf.String()), 0644); err != nil {
		return fmt.Errorf("write %s: %w", filePath, err)
	}

	return nil
}

// setKey sets a value at the given dotted key path, handling array indices.
// Uses an explicit index loop (not for-range) so it can skip array index parts via i++.
func setKey(tree *toml.Tree, key string, value interface{}, allowAdd bool) error {
	parts := strings.Split(key, ".")
	current := interface{}(tree)

	for i := 0; i < len(parts); i++ {
		part := parts[i]
		isLast := i == len(parts)-1

		switch v := current.(type) {
		case *toml.Tree:
			if isLast {
				if !v.Has(part) && !allowAdd {
					return fmt.Errorf("key %q does not exist (use --add to insert)", key)
				}
				v.Set(part, value)
				return nil
			}
			// Peek next part — if numeric, it's an array index
			nextIdx, nextErr := strconv.Atoi(parts[i+1])
			if nextErr == nil {
				arr := v.Get(part)
				arrTrees, ok := arr.([]*toml.Tree)
				if !ok {
					return fmt.Errorf("key %q is not an array of tables", strings.Join(parts[:i+1], "."))
				}
				if nextIdx < 0 || nextIdx >= len(arrTrees) {
					return fmt.Errorf("index %d out of range for %q (length %d)", nextIdx, strings.Join(parts[:i+1], "."), len(arrTrees))
				}
				current = arrTrees[nextIdx]
				i++ // skip the index part
				continue
			}
			next := v.Get(part)
			if next == nil {
				if allowAdd {
					newTree := toml.Tree{}
					v.Set(part, &newTree)
					current = &newTree
				} else {
					return fmt.Errorf("key %q does not exist (use --add to insert)", strings.Join(parts[:i+1], "."))
				}
			} else {
				current = next
			}
		default:
			return fmt.Errorf("key %q is not a table", strings.Join(parts[:i], "."))
		}
	}

	return nil
}
```

- [ ] **Step 4: Run all toml-update tests**

Run: `cd cmd/onepkg && go test -run "TestRunTomlUpdate|TestParseValue" -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/toml_update.go cmd/onepkg/toml_update_test.go
git commit -m "feat(toml-update): support array index via dotted paths"
```

---

### Task 5: CLI dispatch in main.go

**Files:**
- Modify: `cmd/onepkg/main.go`

- [ ] **Step 1: Modify main() to detect toml-update subcommand**

In `cmd/onepkg/main.go`, insert at line 15 (after `if len(os.Args) < 2`), before the flag parsing loop:

```go
// Subcommand dispatch
if os.Args[1] == "toml-update" {
	var addMode bool
	rest := os.Args[2:]
	if len(rest) > 0 && rest[0] == "--add" {
		addMode = true
		rest = rest[1:]
	}
	var err error
	if addMode {
		err = runTomlUpdateAdd(rest)
	} else {
		err = runTomlUpdate(rest)
	}
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	return
}
```

- [ ] **Step 2: Update help text**

In the `printHelp()` function, add a subcommand section:

```go
func printHelp() {
	fmt.Println(`onepkg - General-purpose self-extracting installer generator

Usage:
  onepkg -c config.toml --platform <platforms> [options]
  onepkg toml-update [--add] <key> <value> <file>

Subcommands:
  toml-update           Update a key/value in a TOML file
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
  onepkg toml-update --add newkey "value" config.toml`)
}
```

- [ ] **Step 3: Run all tests**

Run: `cd cmd/onepkg && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 4: Manual smoke test**

Run: `cd cmd/onepkg && go build -o onepkg . && echo 'name = "test"' > /tmp/test.toml && ./onepkg toml-update name "updated" /tmp/test.toml && cat /tmp/test.toml`
Expected output: `name = "updated"`

Then: `./onepkg toml-update --add newkey "newval" /tmp/test.toml && cat /tmp/test.toml`
Expected: contains both `name = "updated"` and `newkey = "newval"`

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/main.go
git commit -m "feat(toml-update): wire up CLI dispatch and help text"
```

---

### Task 6: Format preservation test

**Files:**
- Test: `cmd/onepkg/toml_update_test.go`

- [ ] **Step 1: Write test verifying comments and ordering preserved**

Append to `cmd/onepkg/toml_update_test.go`:

```go
func TestRunTomlUpdate_FormatPreservation(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := `# This is a comment
name = "old"
version = "v1.0.0" # inline comment

[services]
name = "myservice"
`
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdate([]string{"name", "new", path})
	if err != nil {
		t.Fatalf("runTomlUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)

	// Comments preserved
	if !strings.Contains(s, "# This is a comment") {
		t.Error("top-level comment not preserved")
	}
	// Version preserved
	if !strings.Contains(s, `version = "v1.0.0"`) {
		t.Error("version not preserved")
	}
	// Services section preserved
	if !strings.Contains(s, `name = "myservice"`) {
		t.Error("services.name not preserved")
	}
	// Value updated
	if !strings.Contains(s, `name = "new"`) {
		t.Errorf("name not updated, got: %s", s)
	}
}
```

- [ ] **Step 2: Run test**

Run: `cd cmd/onepkg && go test -run TestRunTomlUpdate_FormatPreservation -v`
Expected: PASS (BurntSushi Tree preserves comments)

- [ ] **Step 3: Commit**

```bash
git add cmd/onepkg/toml_update_test.go
git commit -m "test(toml-update): verify comment and format preservation"
```

---

### Task 7: Final validation

- [ ] **Step 1: Run all tests**

Run: `cd cmd/onepkg && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 2: Build and smoke test with real config**

Run: `cd cmd/onepkg && go build -o onepkg . && ./onepkg toml-update version "2.0.0" ../../config.toml && head -5 ../../config.toml`
Expected: version field updated

- [ ] **Step 3: Final commit (if any fixes needed)**

```bash
git add -A
git commit -m "test(toml-update): final validation and fixes"
```
