package main

import "testing"

func TestParseStubArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantAction string
		wantDir    string
	}{
		{"defaults when empty", nil, "install", ""},
		{"bare action keyword", []string{"uninstall"}, "uninstall", ""},
		{"-Action flag", []string{"-Action", "verify"}, "verify", ""},
		{"-Action flag lower-case", []string{"-action", "list"}, "list", ""},
		{"-InstallDir flag", []string{"-InstallDir", `D:\apps`}, "install", `D:\apps`},
		{"action and installdir together", []string{"uninstall", "-InstallDir", `D:\apps`}, "uninstall", `D:\apps`},
		{"unknown bare arg ignored, later action wins", []string{"foo", "install"}, "install", ""},
		{"-Action flag without value is ignored", []string{"-Action"}, "install", ""},
		{"rejects unknown action value", []string{"-Action", "purge"}, "install", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, dir := parseStubArgs(tt.args)
			if action != tt.wantAction {
				t.Errorf("action = %q, want %q", action, tt.wantAction)
			}
			if dir != tt.wantDir {
				t.Errorf("installDir = %q, want %q", dir, tt.wantDir)
			}
		})
	}
}
