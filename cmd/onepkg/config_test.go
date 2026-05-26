package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	content := `
name = "autotest"
version = "v1.0.0"
output_dir = "dist"

items = [
  "bin/test1",
  "bin/test2",
  "etc/gate.conf",
  "data/",
  "res/",
]

[[services]]
name = "main"
exec = "bin/test1"
restart = "on-failure"
user = "autotest"

[services.linux]
type = "simple"
after = ["network.target"]
restart_sec = 5

[services.macos]
run_at_load = true
keep_alive = true
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	if cfg.Name != "autotest" {
		t.Errorf("Name = %q, want %q", cfg.Name, "autotest")
	}
	if cfg.Version != "v1.0.0" {
		t.Errorf("Version = %q, want %q", cfg.Version, "v1.0.0")
	}
	if cfg.OutputDir != "dist" {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, "dist")
	}
	if len(cfg.Items) != 5 {
		t.Errorf("len(Items) = %d, want 5", len(cfg.Items))
	}
	if cfg.Items[0] != "bin/test1" {
		t.Errorf("Items[0] = %q, want %q", cfg.Items[0], "bin/test1")
	}
	if len(cfg.Services) != 1 {
		t.Fatalf("len(Services) = %d, want 1", len(cfg.Services))
	}
	svc := cfg.Services[0]
	if svc.Name != "main" {
		t.Errorf("Services[0].Name = %q, want %q", svc.Name, "main")
	}
	if svc.Exec != "bin/test1" {
		t.Errorf("Services[0].Exec = %q, want %q", svc.Exec, "bin/test1")
	}
	if svc.Restart != "on-failure" {
		t.Errorf("Services[0].Restart = %q, want %q", svc.Restart, "on-failure")
	}
	if svc.User != "autotest" {
		t.Errorf("Services[0].User = %q, want %q", svc.User, "autotest")
	}
	if svc.Linux.Type != "simple" {
		t.Errorf("Services[0].Linux.Type = %q, want %q", svc.Linux.Type, "simple")
	}
	if len(svc.Linux.After) != 1 || svc.Linux.After[0] != "network.target" {
		t.Errorf("Services[0].Linux.After = %v, want [network.target]", svc.Linux.After)
	}
	if svc.Linux.RestartSec != 5 {
		t.Errorf("Services[0].Linux.RestartSec = %d, want 5", svc.Linux.RestartSec)
	}
	if !svc.MacOS.RunAtLoad {
		t.Error("Services[0].MacOS.RunAtLoad = false, want true")
	}
	if !svc.MacOS.KeepAlive {
		t.Error("Services[0].MacOS.KeepAlive = false, want true")
	}
}

func TestParseConfigMinimal(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	content := `
name = "myapp"
version = "v0.1.0"

items = ["bin/myapp"]
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	if cfg.Name != "myapp" {
		t.Errorf("Name = %q, want %q", cfg.Name, "myapp")
	}
	if len(cfg.Items) != 1 {
		t.Errorf("len(Items) = %d, want 1", len(cfg.Items))
	}
	if len(cfg.Services) != 0 {
		t.Errorf("len(Services) = %d, want 0", len(cfg.Services))
	}
	if len(cfg.Platforms) != 0 {
		t.Errorf("len(Platforms) = %d, want 0 (must be provided via --platform)", len(cfg.Platforms))
	}
}

func TestParseConfigFileNotFound(t *testing.T) {
	_, err := ParseConfig("/nonexistent/config.toml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestApplyDefaults(t *testing.T) {
	cfg := &Config{
		Name:    "myapp",
		Version: "v1.0.0",
		Items:   []string{"bin/myapp"},
		Services: []ServiceConfig{
			{Exec: "bin/myapp"},
		},
	}

	cfg.ApplyDefaults()

	if cfg.OutputDir != "packages" {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, "packages")
	}
	// Platforms should NOT have a default - must be provided via CLI
	if len(cfg.Platforms) != 0 {
		t.Errorf("len(Platforms) = %d, want 0 (platforms must be provided via --platform)", len(cfg.Platforms))
	}
	svc := cfg.Services[0]
	if svc.Name != "myapp" {
		t.Errorf("Services[0].Name = %q, want %q (package name)", svc.Name, "myapp")
	}
	if svc.Restart != "on-failure" {
		t.Errorf("Services[0].Restart = %q, want %q", svc.Restart, "on-failure")
	}
	if svc.User != "root" {
		t.Errorf("Services[0].User = %q, want %q", svc.User, "root")
	}
}

func TestValidate(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "myapp"), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{
			name: "valid config",
			cfg: &Config{
				Name:    "myapp",
				Version: "v1.0.0",
				Items:   []string{"bin/myapp"},
				Services: []ServiceConfig{
					{Name: "myapp", Exec: "bin/myapp"},
				},
			},
			wantErr: "",
		},
		{
			name:    "empty name",
			cfg:     &Config{Name: "", Version: "v1.0.0", Items: []string{"bin/myapp"}},
			wantErr: "name is required",
		},
		{
			name:    "empty version",
			cfg:     &Config{Name: "myapp", Version: "", Items: []string{"bin/myapp"}},
			wantErr: "version is required",
		},
		{
			name:    "empty items",
			cfg:     &Config{Name: "myapp", Version: "v1.0.0", Items: []string{}},
			wantErr: "at least one item is required",
		},
		{
			name:    "path traversal prefix",
			cfg:     &Config{Name: "myapp", Version: "v1.0.0", Items: []string{"../../../etc/passwd"}},
			wantErr: "path traversal",
		},
		{
			name:    "path traversal mid path",
			cfg:     &Config{Name: "myapp", Version: "v1.0.0", Items: []string{"foo/../../etc/passwd"}},
			wantErr: "path traversal",
		},
		{
			name:    "dotdot in filename allowed",
			cfg:     &Config{Name: "myapp", Version: "v1.0.0", Items: []string{"bin/my..app"}},
			wantErr: "does not exist",
		},
		{
			name:    "absolute path",
			cfg:     &Config{Name: "myapp", Version: "v1.0.0", Items: []string{"/etc/passwd"}},
			wantErr: "absolute path",
		},
		{
			name: "missing item file",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/nonexistent"},
			},
			wantErr: "does not exist",
		},
		{
			name: "service exec not in items",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items:    []string{"bin/myapp"},
				Services: []ServiceConfig{{Name: "myapp", Exec: "bin/other"}},
			},
			wantErr: "service exec \"bin/other\" not found in items",
		},
		{
			name: "service name with slash",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items:    []string{"bin/myapp"},
				Services: []ServiceConfig{{Name: "../evil", Exec: "bin/myapp"}},
			},
			wantErr: "service name",
		},
		{
			name: "service name with space",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items:    []string{"bin/myapp"},
				Services: []ServiceConfig{{Name: "my app", Exec: "bin/myapp"}},
			},
			wantErr: "service name",
		},
		{
			name: "service name valid with hyphen",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items:    []string{"bin/myapp"},
				Services: []ServiceConfig{{Name: "my-app", Exec: "bin/myapp"}},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate(dir)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() returned unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() returned nil, want error containing %q", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %q, want error containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestHasPlatformMarker(t *testing.T) {
	tests := []struct {
		item     string
		expected bool
	}{
		{"bin/myapp.{platform}", true},
		{"bin/helper.{platform}", true},
		{"bin/myapp", false},
		{"etc/config.conf", false},
		{"data/", false},
		{"lib/plugin.{platform}.so", true},
		{"bin/myapp.linux-arm64", false},
	}

	for _, tt := range tests {
		t.Run(tt.item, func(t *testing.T) {
			result := HasPlatformMarker(tt.item)
			if result != tt.expected {
				t.Errorf("HasPlatformMarker(%q) = %v, want %v", tt.item, result, tt.expected)
			}
		})
	}
}

func TestResolvePlatformItem(t *testing.T) {
	tests := []struct {
		item     string
		platform string
		expected string
	}{
		{"bin/myapp.{platform}", "linux-arm64", "bin/myapp.linux-arm64"},
		{"bin/helper.{platform}", "darwin-arm64", "bin/helper.darwin-arm64"},
		{"lib/plugin.{platform}.so", "linux-amd64", "lib/plugin.linux-amd64.so"},
		{"bin/myapp", "linux-arm64", "bin/myapp"},
		{"etc/config.conf", "darwin-arm64", "etc/config.conf"},
	}

	for _, tt := range tests {
		t.Run(tt.item+"_"+tt.platform, func(t *testing.T) {
			result := ResolvePlatformItem(tt.item, tt.platform)
			if result != tt.expected {
				t.Errorf("ResolvePlatformItem(%q, %q) = %q, want %q", tt.item, tt.platform, result, tt.expected)
			}
		})
	}
}

func TestStripPlatformMarker(t *testing.T) {
	tests := []struct {
		item     string
		expected string
	}{
		{"bin/myapp.{platform}", "bin/myapp"},
		{"bin/helper.{platform}", "bin/helper"},
		{"lib/plugin.{platform}.so", "lib/plugin.so"},
		{"bin/myapp", "bin/myapp"},
		{"etc/config.conf", "etc/config.conf"},
	}

	for _, tt := range tests {
		t.Run(tt.item, func(t *testing.T) {
			result := StripPlatformMarker(tt.item)
			if result != tt.expected {
				t.Errorf("StripPlatformMarker(%q) = %q, want %q", tt.item, result, tt.expected)
			}
		})
	}
}

func TestValidatePlatformMarker(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create platform-specific files
	if err := os.WriteFile(filepath.Join(binDir, "myapp.linux-arm64"), []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	// Do NOT create the {platform} literal - it shouldn't be checked

	tests := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{
			name: "platform marker skipped in validation",
			cfg: &Config{
				Name:    "myapp",
				Version: "v1.0.0",
				Items:   []string{"bin/myapp.{platform}"},
			},
			wantErr: "", // Should NOT error - {platform} items skip existence check
		},
		{
			name: "mixed items - literal checked, marker skipped",
			cfg: &Config{
				Name:    "myapp",
				Version: "v1.0.0",
				Items:   []string{"bin/myapp.{platform}", "etc/config.conf"},
			},
			wantErr: "does not exist", // Literal item should be checked
		},
		{
			name: "service exec matches stripped platform marker",
			cfg: &Config{
				Name:    "myapp",
				Version: "v1.0.0",
				Items:   []string{"bin/myapp.{platform}"},
				Services: []ServiceConfig{{Name: "myapp", Exec: "bin/myapp"}},
			},
			wantErr: "", // Exec "bin/myapp" matches stripped marker
		},
		{
			name: "service exec does not match stripped marker",
			cfg: &Config{
				Name:    "myapp",
				Version: "v1.0.0",
				Items:   []string{"bin/myapp.{platform}"},
				Services: []ServiceConfig{{Name: "myapp", Exec: "bin/other"}},
			},
			wantErr: "service exec \"bin/other\" not found in items",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate(dir)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() returned unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() returned nil, want error containing %q", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %q, want error containing %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestServiceExecPlatformMarkerRejected(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		Name:    "myapp",
		Version: "v1.0.0",
		Items:   []string{"bin/myapp.{platform}"},
		Services: []ServiceConfig{{Name: "myapp", Exec: "bin/myapp.{platform}"}},
	}

	err := cfg.Validate(dir)
	if err == nil {
		t.Error("expected error for service exec with {platform} marker")
	}
	if !strings.Contains(err.Error(), "{platform} marker") {
		t.Errorf("error should mention {platform} marker, got: %v", err)
	}
}

func TestParseConfigWithArgs(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	content := `
name = "myapp"
version = "v1.0.0"

items = ["bin/myapp"]

[[services]]
name = "myapp"
exec = "bin/myapp"
args = ["--config", "etc/myapp.conf", "--log-level", "info"]
restart = "on-failure"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	if len(cfg.Services) != 1 {
		t.Fatalf("len(Services) = %d, want 1", len(cfg.Services))
	}
	svc := cfg.Services[0]
	if len(svc.Args) != 4 {
		t.Errorf("len(Args) = %d, want 4", len(svc.Args))
	}
	if svc.Args[0] != "--config" {
		t.Errorf("Args[0] = %q, want %q", svc.Args[0], "--config")
	}
	if svc.Args[1] != "etc/myapp.conf" {
		t.Errorf("Args[1] = %q, want %q", svc.Args[1], "etc/myapp.conf")
	}
	if svc.Args[2] != "--log-level" {
		t.Errorf("Args[2] = %q, want %q", svc.Args[2], "--log-level")
	}
	if svc.Args[3] != "info" {
		t.Errorf("Args[3] = %q, want %q", svc.Args[3], "info")
	}
}

func TestParseConfigEmptyArgs(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	content := `
name = "myapp"
version = "v1.0.0"

items = ["bin/myapp"]

[[services]]
name = "myapp"
exec = "bin/myapp"
restart = "on-failure"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	if len(cfg.Services) != 1 {
		t.Fatalf("len(Services) = %d, want 1", len(cfg.Services))
	}
	svc := cfg.Services[0]
	if len(svc.Args) != 0 {
		t.Errorf("len(Args) = %d, want 0 (no args field in config)", len(svc.Args))
	}
}

func TestParseConfigWithWorkingDir(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	content := `
name = "myapp"
version = "v1.0.0"

items = ["bin/myapp"]

[[services]]
name = "myapp"
exec = "bin/myapp"

[services.linux]
working_dir = "/var/lib/myapp"

[services.macos]
working_dir = "/opt/myapp/data"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	if cfg.Services[0].Linux.WorkingDir != "/var/lib/myapp" {
		t.Errorf("Linux.WorkingDir = %q, want %q", cfg.Services[0].Linux.WorkingDir, "/var/lib/myapp")
	}
	if cfg.Services[0].MacOS.WorkingDir != "/opt/myapp/data" {
		t.Errorf("MacOS.WorkingDir = %q, want %q", cfg.Services[0].MacOS.WorkingDir, "/opt/myapp/data")
	}
}

func TestParseConfigWithPreservePermissions(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	// Test explicit true
	content := `
name = "myapp"
version = "v1.0.0"
preserve_permissions = true
items = ["bin/myapp"]
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	if cfg.PreservePermissions == nil || !*cfg.PreservePermissions {
		t.Errorf("PreservePermissions = %v, want true", cfg.PreservePermissions)
	}

	// Test explicit false
	contentFalse := `
name = "myapp"
version = "v1.0.0"
preserve_permissions = false
items = ["bin/myapp"]
`
	configPath2 := filepath.Join(dir, "config2.toml")
	if err := os.WriteFile(configPath2, []byte(contentFalse), 0644); err != nil {
		t.Fatal(err)
	}

	cfg2, err := ParseConfig(configPath2)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	if cfg2.PreservePermissions == nil || *cfg2.PreservePermissions {
		t.Errorf("PreservePermissions = %v, want false", cfg2.PreservePermissions)
	}

	// Test not specified (nil before ApplyDefaults)
	contentNil := `
name = "myapp"
version = "v1.0.0"
items = ["bin/myapp"]
`
	configPath3 := filepath.Join(dir, "config3.toml")
	if err := os.WriteFile(configPath3, []byte(contentNil), 0644); err != nil {
		t.Fatal(err)
	}

	cfg3, err := ParseConfig(configPath3)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	// Before ApplyDefaults, should be nil
	if cfg3.PreservePermissions != nil {
		t.Errorf("PreservePermissions before ApplyDefaults = %v, want nil", cfg3.PreservePermissions)
	}

	cfg3.ApplyDefaults()

	// After ApplyDefaults, should be true
	if cfg3.PreservePermissions == nil || !*cfg3.PreservePermissions {
		t.Errorf("PreservePermissions after ApplyDefaults = %v, want true", cfg3.PreservePermissions)
	}
}
