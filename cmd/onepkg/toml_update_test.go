package main

import (
	"os"
	"reflect"
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
		{"null", nil},
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

func TestRunTomlUpdate_Add_PreservesFormat(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := "# header\nname = \"old\"\nversion = \"v1\"\n\n[services]\nname = \"svc\"\n"
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdateAdd([]string{"newkey", "newval", path})
	if err != nil {
		t.Fatalf("runTomlUpdateAdd: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "# header") {
		t.Error("comment not preserved after --add")
	}
	if !strings.Contains(s, `version = "v1"`) {
		t.Error("version not preserved after --add")
	}
	if !strings.Contains(s, `name = "svc"`) {
		t.Error("services.name not preserved after --add")
	}
	if !strings.Contains(s, `newkey = "newval"`) {
		t.Errorf("new key not added, got: %s", s)
	}
	// Ensure original order is preserved (name before version)
	nameIdx := strings.Index(s, `name = "old"`)
	versionIdx := strings.Index(s, `version = "v1"`)
	if nameIdx == -1 || versionIdx == -1 {
		t.Fatalf("missing original keys, got: %s", s)
	}
	if nameIdx > versionIdx {
		t.Error("key order not preserved after --add")
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

func TestRunTomlUpdate_ArraySecondElement(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := `[[dirs]]
path = "/var/log"
owner = "root"

[[dirs]]
path = "/var/lib"
owner = "app"
`
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdate([]string{"dirs.1.owner", "nobody", path})
	if err != nil {
		t.Fatalf("runTomlUpdate: %v", err)
	}

	tree, _ := toml.LoadFile(path)
	arr := tree.Get("dirs").([]*toml.Tree)
	if arr[0].Get("owner") != "root" {
		t.Errorf("dirs.0.owner should be unchanged, got %v", arr[0].Get("owner"))
	}
	if arr[1].Get("owner") != "nobody" {
		t.Errorf("dirs.1.owner = %v, want nobody", arr[1].Get("owner"))
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

func TestRunTomlUpdate_NestedKeyCollision(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := "# header\nname = \"toplevel\"\n\n[services]\nname = \"svcname\"\n"
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdate([]string{"services.name", "new", path})
	if err != nil {
		t.Fatalf("runTomlUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, `name = "toplevel"`) {
		t.Errorf("top-level name should be unchanged, got: %s", s)
	}
	if !strings.Contains(s, `name = "new"`) {
		t.Errorf("services.name should be updated, got: %s", s)
	}
	if !strings.Contains(s, "# header") {
		t.Error("comment not preserved")
	}
}

func TestRunTomlUpdate_StringWithQuotes(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	os.WriteFile(path, []byte(`msg = "hello world"`+"\n"), 0644)

	err := runTomlUpdate([]string{"msg", `say "hi"`, path})
	if err != nil {
		t.Fatalf("runTomlUpdate: %v", err)
	}

	tree, err := toml.LoadFile(path)
	if err != nil {
		t.Fatalf("result is not valid TOML: %v\ncontent: %s", err, mustRead(path))
	}
	if tree.Get("msg") != `say "hi"` {
		t.Errorf("msg = %v, want %q", tree.Get("msg"), `say "hi"`)
	}
}

func TestRunTomlUpdate_StringWithBackslash(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	os.WriteFile(path, []byte(`path = "/usr/bin"`+"\n"), 0644)

	err := runTomlUpdate([]string{"path", `C:\Users\test`, path})
	if err != nil {
		t.Fatalf("runTomlUpdate: %v", err)
	}

	tree, err := toml.LoadFile(path)
	if err != nil {
		t.Fatalf("result is not valid TOML: %v\ncontent: %s", err, mustRead(path))
	}
	if tree.Get("path") != `C:\Users\test` {
		t.Errorf("path = %v, want %q", tree.Get("path"), `C:\Users\test`)
	}
}

func TestRunTomlUpdate_DottedInlineKey(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := `server = "https://example.com"
log.to = "file"
log.level = "info"
`
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdate([]string{"log.to", "stdout", path})
	if err != nil {
		t.Fatalf("runTomlUpdate: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, `log.to = "stdout"`) {
		t.Errorf("log.to not updated, got: %s", s)
	}
	if !strings.Contains(s, `log.level = "info"`) {
		t.Error("log.level should be unchanged")
	}
	if !strings.Contains(s, `server = "https://example.com"`) {
		t.Error("server should be unchanged")
	}
}

func TestRunTomlUpdate_StringValuePreserved(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := `name = "true"
mode = "0750"
ver = "1.0"
`
	os.WriteFile(path, []byte(content), 0644)

	tests := []struct {
		key      string
		value    string
		expected string
	}{
		{"name", "false", `name = "false"`},
		{"mode", "0644", `mode = "0644"`},
		{"ver", "2.0", `ver = "2.0"`},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			err := runTomlUpdate([]string{tt.key, tt.value, path})
			if err != nil {
				t.Fatalf("runTomlUpdate(%q, %q): %v", tt.key, tt.value, err)
			}
			data, _ := os.ReadFile(path)
			s := string(data)
			if !strings.Contains(s, tt.expected) {
				t.Errorf("got %q, want to contain %q", s, tt.expected)
			}
			// Verify the result is valid TOML
			tree, err := toml.Load(s)
			if err != nil {
				t.Fatalf("result is not valid TOML: %v\ncontent: %s", err, s)
			}
			got := tree.Get(tt.key)
			if got != tt.value {
				t.Errorf("%s = %v (%T), want %q (string)", tt.key, got, got, tt.value)
			}
		})
	}
}

func TestRunTomlUpdate_SameValue(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	os.WriteFile(path, []byte("name = \"old\"\ncount = 5\nenabled = true\n"), 0644)

	tests := []struct {
		key   string
		value string
	}{
		{"name", "old"},
		{"count", "5"},
		{"enabled", "true"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			original, _ := os.ReadFile(path)
			err := runTomlUpdate([]string{tt.key, tt.value, path})
			if err != nil {
				t.Errorf("runTomlUpdate(%q, %q): unexpected error: %v", tt.key, tt.value, err)
			}
			after, _ := os.ReadFile(path)
			if string(original) != string(after) {
				t.Errorf("file should not change when value is the same\nbefore: %q\nafter: %q", string(original), string(after))
			}
		})
	}
}

func TestRunTomlUpdate_Add_TopLevelBeforeSections(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := "name = \"old\"\n\n[services]\nname = \"svc\"\n"
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdateAdd([]string{"newkey", "newval", path})
	if err != nil {
		t.Fatalf("runTomlUpdateAdd: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)

	// newkey must be a top-level key, not part of [services]
	tree, err := toml.Load(s)
	if err != nil {
		t.Fatalf("result is not valid TOML: %v\ncontent: %s", err, s)
	}
	if tree.Get("newkey") != "newval" {
		t.Errorf("newkey should be top-level, got services.newkey = %v", tree.Get("newkey"))
	}
}

func TestRunTomlUpdate_Add_TopLevelBeforeSectionComment(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := "name = \"old\"\n\n# Services section\n[services]\nname = \"svc\"\n"
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdateAdd([]string{"newkey", "newval", path})
	if err != nil {
		t.Fatalf("runTomlUpdateAdd: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)

	newkeyIdx := strings.Index(s, `newkey = "newval"`)
	commentIdx := strings.Index(s, "# Services section")
	if newkeyIdx == -1 {
		t.Fatalf("newkey not found in: %s", s)
	}
	if commentIdx == -1 {
		t.Fatalf("comment not found in: %s", s)
	}
	if newkeyIdx > commentIdx {
		t.Errorf("newkey should be before section comment, got:\n%s", s)
	}
}

func TestRunTomlUpdate_Add_SectionKeyBeforeNextSectionComment(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := "[services]\nname = \"svc\"\n\n# Logging\n[logging]\nlevel = \"info\"\n"
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdateAdd([]string{"services.port", "8080", path})
	if err != nil {
		t.Fatalf("runTomlUpdateAdd: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)

	portIdx := strings.Index(s, "port = 8080")
	commentIdx := strings.Index(s, "# Logging")
	if portIdx == -1 {
		t.Fatalf("port not found in: %s", s)
	}
	if commentIdx == -1 {
		t.Fatalf("# Logging not found in: %s", s)
	}
	if portIdx > commentIdx {
		t.Errorf("new key should be before # Logging comment, got:\n%s", s)
	}
}

func TestRunTomlUpdate_Add_DottedInlineKey(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := `server = "https://example.com"
agent_id = "test001"

# logger
log.to = "stdout"
log.level = "info"
log.file = "/var/log/agent.log"
`
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdateAdd([]string{"log.group2", "test2", path})
	if err != nil {
		t.Fatalf("runTomlUpdateAdd: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)

	tree, err := toml.Load(s)
	if err != nil {
		t.Fatalf("result is not valid TOML: %v\ncontent: %s", err, s)
	}
	if tree.Get("log.group2") != "test2" {
		t.Errorf("log.group2 = %v, want test2", tree.Get("log.group2"))
	}
	if tree.Get("log.level") != "info" {
		t.Error("log.level should be unchanged")
	}
	if tree.Get("server") != "https://example.com" {
		t.Error("server should be unchanged")
	}

	// The new key should appear after the last log.* key
	lastLogIdx := strings.LastIndex(s, "log.file")
	newKeyIdx := strings.Index(s, "log.group2")
	if newKeyIdx < lastLogIdx {
		t.Errorf("log.group2 should appear after last log.* key, got:\n%s", s)
	}
}

func TestRunTomlUpdate_Add_DeepDottedInlineKey(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := `name = "myapp"
a.b.x = "1"
a.b.y = "2"
`
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdateAdd([]string{"a.b.c", "three", path})
	if err != nil {
		t.Fatalf("runTomlUpdateAdd: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)

	tree, err := toml.Load(s)
	if err != nil {
		t.Fatalf("result is not valid TOML: %v\ncontent: %s", err, s)
	}
	if tree.Get("a.b.c") != "three" {
		t.Errorf("a.b.c = %v, want three", tree.Get("a.b.c"))
	}
}

func TestRunTomlUpdate_Add_SectionHeaderPriorityOverInline(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := `name = "myapp"

[log]
to = "stdout"
level = "info"
`
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdateAdd([]string{"log.file", "/tmp/log", path})
	if err != nil {
		t.Fatalf("runTomlUpdateAdd: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)

	// When section header exists, the leaf key is used (not dotted)
	if strings.Contains(s, "log.file") {
		t.Errorf("should use leaf key 'file' inside [log] section, not dotted 'log.file', got:\n%s", s)
	}
	if !strings.Contains(s, "file = \"/tmp/log\"") {
		t.Errorf("file key not found in [log] section, got:\n%s", s)
	}
}

func TestValueRepresentations(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want []string
	}{
		{"empty string", "", []string{`""`, `''`}},
		{"plain string", "hello", []string{`"hello"`, `'hello'`}},
		{"string with apostrophe", "it's", []string{`"it's"`}},
		{"string with newline", "a\nb", []string{`"a\nb"`}},
		{"int", int64(5), []string{`5`}},
		{"bool", true, []string{`true`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := valueRepresentations(tt.val)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("valueRepresentations(%v) = %v, want %v", tt.val, got, tt.want)
			}
		})
	}
}

func TestRunTomlUpdate_SingleQuotedString(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	// RustDesk2.toml stores string values as single-quoted literal strings
	// (e.g. custom-rendezvous-server = ''). The updater must locate and
	// replace them regardless of the on-disk quote style.
	content := "[options]\ncustom-rendezvous-server = ''\nkey = ''\n"
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdateAdd([]string{"options.custom-rendezvous-server", "1mtok.com:21116", path})
	if err != nil {
		t.Fatalf("runTomlUpdateAdd: %v", err)
	}

	tree, err := toml.LoadFile(path)
	if err != nil {
		t.Fatalf("result is not valid TOML: %v\ncontent: %s", err, mustRead(path))
	}
	if got := tree.Get("options.custom-rendezvous-server"); got != "1mtok.com:21116" {
		t.Errorf("options.custom-rendezvous-server = %v, want 1mtok.com:21116\ncontent: %s", got, mustRead(path))
	}
	// Sibling key untouched.
	if got := tree.Get("options.key"); got != "" {
		t.Errorf("options.key = %v, want empty string (untouched)", got)
	}
}

func TestRunTomlUpdate_SingleQuotedStringNonEmpty(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	content := "[options]\ncustom-rendezvous-server = 'old.example.com:21116'\n"
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdateAdd([]string{"options.custom-rendezvous-server", "new.example.com:21116", path})
	if err != nil {
		t.Fatalf("runTomlUpdateAdd: %v", err)
	}

	tree, err := toml.LoadFile(path)
	if err != nil {
		t.Fatalf("result is not valid TOML: %v\ncontent: %s", err, mustRead(path))
	}
	if got := tree.Get("options.custom-rendezvous-server"); got != "new.example.com:21116" {
		t.Errorf("got %v, want new.example.com:21116\ncontent: %s", got, mustRead(path))
	}
}

func TestRunTomlUpdate_SingleQuotedAlreadySet(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	// Already set with the same value: the file must be left untouched,
	// preserving its single-quoted style (no spurious rewrite).
	content := "[options]\ncustom-rendezvous-server = '1mtok.com:21116'\n"
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdateAdd([]string{"options.custom-rendezvous-server", "1mtok.com:21116", path})
	if err != nil {
		t.Fatalf("runTomlUpdateAdd: %v", err)
	}

	after, _ := os.ReadFile(path)
	if string(after) != content {
		t.Errorf("file should not change when value already set\nbefore: %q\nafter: %q", content, string(after))
	}
}

func TestRunTomlUpdate_SingleQuotedBackslash(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	// A single-quoted literal string holds a raw backslash (no escaping on
	// disk); the literal-form candidate must match it and the new value must
	// be written as an escaped double-quoted string.
	os.WriteFile(path, []byte("[options]\npath = 'C:\\Users\\test'\n"), 0644)

	err := runTomlUpdateAdd([]string{"options.path", `E:\backup`, path})
	if err != nil {
		t.Fatalf("runTomlUpdateAdd: %v", err)
	}

	tree, err := toml.LoadFile(path)
	if err != nil {
		t.Fatalf("result is not valid TOML: %v\ncontent: %s", err, mustRead(path))
	}
	if got := tree.Get("options.path"); got != `E:\backup` {
		t.Errorf("options.path = %v, want E:\\backup\ncontent: %s", got, mustRead(path))
	}
}

func TestRunTomlUpdate_SingleQuotedTrailingComment(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.toml"
	// Trailing comment on a single-quoted line must survive the rewrite to
	// the canonical double-quoted form.
	content := "[options]\ncustom-rendezvous-server = '' # rustdesk\n"
	os.WriteFile(path, []byte(content), 0644)

	err := runTomlUpdateAdd([]string{"options.custom-rendezvous-server", "1mtok.com:21116", path})
	if err != nil {
		t.Fatalf("runTomlUpdateAdd: %v", err)
	}

	data, _ := os.ReadFile(path)
	s := string(data)
	want := `custom-rendezvous-server = "1mtok.com:21116" # rustdesk`
	if !strings.Contains(s, want) {
		t.Errorf("expected value updated and comment preserved, got:\n%s", s)
	}
	if _, err := toml.Load(s); err != nil {
		t.Fatalf("result is not valid TOML: %v\ncontent: %s", err, s)
	}
}

func mustRead(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}
