package main

import (
	"os"
	"strings"
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
