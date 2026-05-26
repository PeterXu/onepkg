package main

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateVersion(t *testing.T) {
	info := VersionInfo{
		Name:     "autotest",
		Version:  "v1.0.0",
		Platform: "linux-arm64",
		Created:  time.Date(2026, 5, 26, 10, 30, 0, 0, time.UTC),
		Files:    5,
		SHA: map[string]string{
			"bin/test1":     "a1b2c3d4e5",
			"etc/gate.conf": "789abcdef",
		},
	}

	output, err := GenerateVersion(info)
	if err != nil {
		t.Fatalf("GenerateVersion returned error: %v", err)
	}

	tests := []struct{ substr string }{
		{`name = "autotest"`},
		{`version = "v1.0.0"`},
		{`platform = "linux-arm64"`},
		{`created = "2026-05-26T10:30:00Z"`},
		{`files = 5`},
		{`[sha]`},
		{`"bin/test1" = "a1b2c3d4e5"`},
		{`"etc/gate.conf" = "789abcdef"`},
	}

	for _, tt := range tests {
		if !strings.Contains(output, tt.substr) {
			t.Errorf("output missing %q\nfull output:\n%s", tt.substr, output)
		}
	}
}

func TestGenerateVersionEmptySHA(t *testing.T) {
	info := VersionInfo{
		Name:     "myapp",
		Version:  "v0.1.0",
		Platform: "darwin-arm64",
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Files:    0,
		SHA:      map[string]string{},
	}

	output, err := GenerateVersion(info)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, `[sha]`) {
		t.Error("output missing [sha] section")
	}
}
