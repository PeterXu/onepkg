package main

import (
	"os"
	"strings"
	"testing"

	"github.com/pelletier/go-toml"
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

func TestRunTomlUpdate_FormatPreservation(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := "# This is a comment\nname = \"old\"\nversion = \"v1.0.0\"\n\n[services]\nname = \"myservice\"\n"
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdate([]string{"name", "new", path})
	if err != nil {
		t.Fatalf("runTomlUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)

	if !strings.Contains(s, "# This is a comment") {
		t.Error("top-level comment not preserved")
	}
	if !strings.Contains(s, `version = "v1.0.0"`) {
		t.Error("version not preserved")
	}
	if !strings.Contains(s, `name = "myservice"`) {
		t.Error("services.name not preserved")
	}
	if !strings.Contains(s, `name = "new"`) {
		t.Errorf("name not updated, got: %s", s)
	}
}
