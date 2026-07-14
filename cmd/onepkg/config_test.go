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
				Name:     "myapp",
				Version:  "v1.0.0",
				Items:    []string{"bin/myapp.{platform}"},
				Services: []ServiceConfig{{Name: "myapp", Exec: "bin/myapp"}},
			},
			wantErr: "", // Exec "bin/myapp" matches stripped marker
		},
		{
			name: "service exec does not match stripped marker",
			cfg: &Config{
				Name:     "myapp",
				Version:  "v1.0.0",
				Items:    []string{"bin/myapp.{platform}"},
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
		Name:     "myapp",
		Version:  "v1.0.0",
		Items:    []string{"bin/myapp.{platform}"},
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

func TestParseConfigWithExclude(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	content := `
name = "myapp"
version = "v1.0.0"

exclude = ["tests/", "*.md", ".git*"]

items = ["bin/myapp", "lib/", "docs/"]
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	if len(cfg.Exclude) != 3 {
		t.Errorf("len(Exclude) = %d, want 3", len(cfg.Exclude))
	}
	if cfg.Exclude[0] != "tests/" {
		t.Errorf("Exclude[0] = %q, want %q", cfg.Exclude[0], "tests/")
	}
	if cfg.Exclude[1] != "*.md" {
		t.Errorf("Exclude[1] = %q, want %q", cfg.Exclude[1], "*.md")
	}
	if cfg.Exclude[2] != ".git*" {
		t.Errorf("Exclude[2] = %q, want %q", cfg.Exclude[2], ".git*")
	}
}

func TestParseConfigExcludeEmpty(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	// No exclude field
	content := `
name = "myapp"
version = "v1.0.0"
items = ["bin/myapp"]
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	if len(cfg.Exclude) != 0 {
		t.Errorf("len(Exclude) = %d, want 0 (no exclude field)", len(cfg.Exclude))
	}
}

func TestParseConfigWithEnvironment(t *testing.T) {
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
environment = { LOG_LEVEL = "info", PORT = "8080" }

[services.macos]
environment = { LOG_LEVEL = "debug" }
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

	if len(svc.Linux.Environment) != 2 {
		t.Errorf("len(Linux.Environment) = %d, want 2", len(svc.Linux.Environment))
	}
	if svc.Linux.Environment["LOG_LEVEL"] != "info" {
		t.Errorf("Linux.Environment[LOG_LEVEL] = %q, want %q", svc.Linux.Environment["LOG_LEVEL"], "info")
	}
	if svc.Linux.Environment["PORT"] != "8080" {
		t.Errorf("Linux.Environment[PORT] = %q, want %q", svc.Linux.Environment["PORT"], "8080")
	}

	if len(svc.MacOS.Environment) != 1 {
		t.Errorf("len(MacOS.Environment) = %d, want 1", len(svc.MacOS.Environment))
	}
	if svc.MacOS.Environment["LOG_LEVEL"] != "debug" {
		t.Errorf("MacOS.Environment[LOG_LEVEL] = %q, want %q", svc.MacOS.Environment["LOG_LEVEL"], "debug")
	}
}

func TestParseConfigWithLimitNOFILE(t *testing.T) {
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
limit_nofile = 65536

[services.macos]
limit_nofile = 10240
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

	if svc.Linux.LimitNOFILE != 65536 {
		t.Errorf("Linux.LimitNOFILE = %d, want 65536", svc.Linux.LimitNOFILE)
	}
	if svc.MacOS.LimitNOFILE != 10240 {
		t.Errorf("MacOS.LimitNOFILE = %d, want 10240", svc.MacOS.LimitNOFILE)
	}
}

func TestParseConfigEnvironmentEmpty(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	// No environment field
	content := `
name = "myapp"
version = "v1.0.0"
items = ["bin/myapp"]

[[services]]
name = "myapp"
exec = "bin/myapp"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	if len(cfg.Services[0].Linux.Environment) != 0 {
		t.Errorf("len(Linux.Environment) = %d, want 0", len(cfg.Services[0].Linux.Environment))
	}
	if cfg.Services[0].Linux.LimitNOFILE != 0 {
		t.Errorf("Linux.LimitNOFILE = %d, want 0", cfg.Services[0].Linux.LimitNOFILE)
	}
}

func TestParseConfigWithDirs(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	content := `
name = "myapp"
version = "v1.0.0"

items = ["bin/myapp"]

dirs = [
  { path = "/var/log/myapp", owner = "myapp", mode = "0755" },
  { path = "data", owner = "myapp", mode = "0750" },
  { path = "/var/lib/myapp" },
]
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	if len(cfg.Dirs) != 3 {
		t.Fatalf("len(Dirs) = %d, want 3", len(cfg.Dirs))
	}

	// First dir - all fields
	if cfg.Dirs[0].Path != "/var/log/myapp" {
		t.Errorf("Dirs[0].Path = %q, want %q", cfg.Dirs[0].Path, "/var/log/myapp")
	}
	if cfg.Dirs[0].Owner != "myapp" {
		t.Errorf("Dirs[0].Owner = %q, want %q", cfg.Dirs[0].Owner, "myapp")
	}
	if cfg.Dirs[0].Mode != "0755" {
		t.Errorf("Dirs[0].Mode = %q, want %q", cfg.Dirs[0].Mode, "0755")
	}

	// Second dir - relative path
	if cfg.Dirs[1].Path != "data" {
		t.Errorf("Dirs[1].Path = %q, want %q", cfg.Dirs[1].Path, "data")
	}

	// Third dir - minimal
	if cfg.Dirs[2].Path != "/var/lib/myapp" {
		t.Errorf("Dirs[2].Path = %q, want %q", cfg.Dirs[2].Path, "/var/lib/myapp")
	}
	if cfg.Dirs[2].Owner != "" {
		t.Errorf("Dirs[2].Owner = %q, want empty (default applied later)", cfg.Dirs[2].Owner)
	}
}

func TestParseConfigDirsEmpty(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	content := `
name = "myapp"
version = "v1.0.0"
items = ["bin/myapp"]
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	if len(cfg.Dirs) != 0 {
		t.Errorf("len(Dirs) = %d, want 0 (no dirs field)", len(cfg.Dirs))
	}
}

func TestValidateDirs(t *testing.T) {
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
			name: "valid absolute path",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: "/var/log/myapp"}},
			},
			wantErr: "",
		},
		{
			name: "valid relative path",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: "data"}},
			},
			wantErr: "",
		},
		{
			name: "path traversal absolute",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: "/var/../etc/passwd"}},
			},
			wantErr: "path traversal",
		},
		{
			name: "path traversal relative",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: "../etc/passwd"}},
			},
			wantErr: "path traversal",
		},
		{
			name: "critical path /root",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: "/root"}},
			},
			wantErr: "critical system path",
		},
		{
			name: "critical path /etc/passwd",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: "/etc/passwd"}},
			},
			wantErr: "critical system path",
		},
		{
			name: "critical path under /sys",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: "/sys/class"}},
			},
			wantErr: "critical system path",
		},
		{
			name: "valid /etc/myapp allowed",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: "/etc/myapp"}},
			},
			wantErr: "",
		},
		{
			name: "valid /var/log/myapp allowed",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: "/var/log/myapp"}},
			},
			wantErr: "",
		},
		{
			name: "empty path rejected",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: ""}},
			},
			wantErr: "path is required",
		},
		{
			name: "invalid mode format",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: "/var/log/myapp", Mode: "755"}},
			},
			wantErr: "invalid mode",
		},
		{
			name: "valid mode 0755",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: "/var/log/myapp", Mode: "0755"}},
			},
			wantErr: "",
		},
		{
			name: "valid mode 0750",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: "/var/log/myapp", Mode: "0750"}},
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

func TestApplyDefaultsDirs(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *Config
		wantPath  string
		wantOwner string
		wantGroup string
		wantMode  string
	}{
		{
			name: "defaults from service user",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items:    []string{"bin/myapp"},
				Dirs:     []DirConfig{{Path: "/var/log/myapp"}},
				Services: []ServiceConfig{{Name: "myapp", User: "appuser"}},
			},
			wantPath:  "/var/log/myapp",
			wantOwner: "appuser",
			wantGroup: "appuser",
			wantMode:  "0755",
		},
		{
			name: "defaults without service (root)",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: "/var/log/myapp"}},
			},
			wantPath:  "/var/log/myapp",
			wantOwner: "root",
			wantGroup: "root",
			wantMode:  "0755",
		},
		{
			name: "explicit owner preserved",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items:    []string{"bin/myapp"},
				Dirs:     []DirConfig{{Path: "/var/log/myapp", Owner: "loguser"}},
				Services: []ServiceConfig{{Name: "myapp", User: "appuser"}},
			},
			wantPath:  "/var/log/myapp",
			wantOwner: "loguser",
			wantGroup: "loguser",
			wantMode:  "0755",
		},
		{
			name: "explicit group preserved",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: "/var/log/myapp", Owner: "loguser", Group: "loggroup"}},
			},
			wantPath:  "/var/log/myapp",
			wantOwner: "loguser",
			wantGroup: "loggroup",
			wantMode:  "0755",
		},
		{
			name: "explicit mode preserved",
			cfg: &Config{
				Name: "myapp", Version: "v1.0.0",
				Items: []string{"bin/myapp"},
				Dirs:  []DirConfig{{Path: "/var/log/myapp", Mode: "0750"}},
			},
			wantPath:  "/var/log/myapp",
			wantOwner: "root",
			wantGroup: "root",
			wantMode:  "0750",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.ApplyDefaults()
			if len(tt.cfg.Dirs) != 1 {
				t.Fatalf("len(Dirs) = %d, want 1", len(tt.cfg.Dirs))
			}
			d := tt.cfg.Dirs[0]
			if d.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", d.Path, tt.wantPath)
			}
			if d.Owner != tt.wantOwner {
				t.Errorf("Owner = %q, want %q", d.Owner, tt.wantOwner)
			}
			if d.Group != tt.wantGroup {
				t.Errorf("Group = %q, want %q", d.Group, tt.wantGroup)
			}
			if d.Mode != tt.wantMode {
				t.Errorf("Mode = %q, want %q", d.Mode, tt.wantMode)
			}
		})
	}
}

func TestParseConfigWithSystemOverlay(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	content := `
name = "myapp"
version = "v1.0.0"

items = ["bin/myapp", "data/test1.txt", "data/certs/"]

[[system_overlay]]
system = "/etc/agent/test1.txt"
item = "data/test1.txt"

[[system_overlay]]
system = "/etc/certs"
item = "data/certs"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	if len(cfg.SystemOverlay) != 2 {
		t.Fatalf("len(SystemOverlay) = %d, want 2", len(cfg.SystemOverlay))
	}
	if cfg.SystemOverlay[0].System != "/etc/agent/test1.txt" {
		t.Errorf("SystemOverlay[0].System = %q, want %q", cfg.SystemOverlay[0].System, "/etc/agent/test1.txt")
	}
	if cfg.SystemOverlay[0].Item != "data/test1.txt" {
		t.Errorf("SystemOverlay[0].Item = %q, want %q", cfg.SystemOverlay[0].Item, "data/test1.txt")
	}
	if cfg.SystemOverlay[1].System != "/etc/certs" {
		t.Errorf("SystemOverlay[1].System = %q, want %q", cfg.SystemOverlay[1].System, "/etc/certs")
	}
	if cfg.SystemOverlay[1].Item != "data/certs" {
		t.Errorf("SystemOverlay[1].Item = %q, want %q", cfg.SystemOverlay[1].Item, "data/certs")
	}
}

func TestParseConfigSystemOverlayEmpty(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	content := `
name = "myapp"
version = "v1.0.0"
items = ["bin/myapp"]
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}

	if len(cfg.SystemOverlay) != 0 {
		t.Errorf("len(SystemOverlay) = %d, want 0 (no overlay defined)", len(cfg.SystemOverlay))
	}
}

func TestValidateSystemOverlay(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "myapp"), []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "test1.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{
			name: "valid overlay matching item",
			cfg: &Config{
				Name:    "myapp",
				Version: "v1.0.0",
				Items:   []string{"bin/myapp", "data/test1.txt"},
				SystemOverlay: []OverlayConfig{
					{System: "/etc/agent/test1.txt", Item: "data/test1.txt"},
				},
			},
			wantErr: "",
		},
		{
			name: "overlay item not in items",
			cfg: &Config{
				Name:    "myapp",
				Version: "v1.0.0",
				Items:   []string{"bin/myapp"},
				SystemOverlay: []OverlayConfig{
					{System: "/etc/agent/test1.txt", Item: "data/missing.txt"},
				},
			},
			wantErr: "overlay item \"data/missing.txt\" not found in items",
		},
		{
			name: "overlay system not absolute",
			cfg: &Config{
				Name:    "myapp",
				Version: "v1.0.0",
				Items:   []string{"bin/myapp"},
				SystemOverlay: []OverlayConfig{
					{System: "etc/agent/test1.txt", Item: "bin/myapp"},
				},
			},
			wantErr: "must be an absolute path",
		},
		{
			name: "overlay system path traversal",
			cfg: &Config{
				Name:    "myapp",
				Version: "v1.0.0",
				Items:   []string{"bin/myapp"},
				SystemOverlay: []OverlayConfig{
					{System: "/etc/../etc/passwd", Item: "bin/myapp"},
				},
			},
			wantErr: "path traversal",
		},
		{
			name: "overlay system critical path",
			cfg: &Config{
				Name:    "myapp",
				Version: "v1.0.0",
				Items:   []string{"bin/myapp"},
				SystemOverlay: []OverlayConfig{
					{System: "/etc/passwd", Item: "bin/myapp"},
				},
			},
			wantErr: "critical system path",
		},
		{
			name: "overlay system empty",
			cfg: &Config{
				Name:    "myapp",
				Version: "v1.0.0",
				Items:   []string{"bin/myapp"},
				SystemOverlay: []OverlayConfig{
					{System: "", Item: "bin/myapp"},
				},
			},
			wantErr: "overlay system path is required",
		},
		{
			name: "overlay item empty",
			cfg: &Config{
				Name:    "myapp",
				Version: "v1.0.0",
				Items:   []string{"bin/myapp"},
				SystemOverlay: []OverlayConfig{
					{System: "/etc/agent/test1.txt", Item: ""},
				},
			},
			wantErr: "overlay item is required",
		},
		{
			name: "overlay item matches platform-marked item stripped form",
			cfg: &Config{
				Name:    "myapp",
				Version: "v1.0.0",
				Items:   []string{"bin/myapp.{platform}"},
				SystemOverlay: []OverlayConfig{
					{System: "/etc/config.txt", Item: "bin/myapp"},
				},
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

func TestParseConfigWindowsService(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	content := `
name = "myapp"
version = "v1.0.0"

items = ["bin/myapp.exe"]

[[services]]
name = "myapp"
exec = "bin/myapp.exe"

[services.windows]
start_type = "manual"
recovery_delay_ms = 3000
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
	if svc.Windows.StartType != "manual" {
		t.Errorf("Windows.StartType = %q, want %q", svc.Windows.StartType, "manual")
	}
	if svc.Windows.RecoveryDelayMs == nil || *svc.Windows.RecoveryDelayMs != 3000 {
		t.Errorf("Windows.RecoveryDelayMs = %v, want 3000", svc.Windows.RecoveryDelayMs)
	}
}

func TestApplyDefaultsWindowsService(t *testing.T) {
	cfg := &Config{
		Name:    "myapp",
		Version: "v1.0.0",
		Items:   []string{"bin/myapp.exe"},
		Services: []ServiceConfig{
			{Exec: "bin/myapp.exe"},
		},
	}

	cfg.ApplyDefaults()

	svc := cfg.Services[0]
	if svc.Windows.StartType != "auto" {
		t.Errorf("Windows.StartType default = %q, want %q", svc.Windows.StartType, "auto")
	}
	if svc.Windows.RecoveryDelayMs == nil || *svc.Windows.RecoveryDelayMs != 5000 {
		t.Errorf("Windows.RecoveryDelayMs default = %v, want 5000", svc.Windows.RecoveryDelayMs)
	}
}

func TestApplyDefaultsWindowsRecoveryDelayZero(t *testing.T) {
	zero := 0
	cfg := &Config{
		Name:    "myapp",
		Version: "v1.0.0",
		Items:   []string{"bin/myapp.exe"},
		Services: []ServiceConfig{
			{Exec: "bin/myapp.exe", Windows: WindowsService{RecoveryDelayMs: &zero}},
		},
	}
	cfg.ApplyDefaults()
	svc := cfg.Services[0]
	if svc.Windows.RecoveryDelayMs == nil {
		t.Fatal("RecoveryDelayMs is nil, expected non-nil (explicitly set to 0)")
	}
	if *svc.Windows.RecoveryDelayMs != 0 {
		t.Errorf("RecoveryDelayMs = %d, want 0 (explicitly set, should not be overridden to default)", *svc.Windows.RecoveryDelayMs)
	}
}

func TestValidateWindowsServiceStartType(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "myapp.exe"), []byte("exe"), 0755)

	tests := []struct {
		name      string
		startType string
		wantErr   string
	}{
		{"valid auto", "auto", ""},
		{"valid manual", "manual", ""},
		{"valid disabled", "disabled", ""},
		{"invalid value", "badtype", "start_type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Name:    "myapp",
				Version: "v1.0.0",
				Items:   []string{"bin/myapp.exe"},
				Services: []ServiceConfig{
					{Name: "myapp", Exec: "bin/myapp.exe", Windows: WindowsService{StartType: tt.startType}},
				},
			}
			err := cfg.Validate(dir)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() returned unexpected error: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func TestParseConfigRunMode(t *testing.T) {
	dir := t.TempDir()
	configContent := `
name = "cleanup"
version = "v1.0.0"
mode = "run"
require_admin = true

[[commands]]
exec = "bin/cleanup"
args = ["--all", "--force"]

[[commands]]
shell = "netsh advfirewall set allprofiles state off"
`
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(path)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.Mode != "run" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "run")
	}
	if !cfg.RequireAdmin {
		t.Error("RequireAdmin = false, want true")
	}
	if len(cfg.Commands) != 2 {
		t.Fatalf("len(Commands) = %d, want 2", len(cfg.Commands))
	}
	if cfg.Commands[0].Exec != "bin/cleanup" {
		t.Errorf("Commands[0].Exec = %q, want %q", cfg.Commands[0].Exec, "bin/cleanup")
	}
	if len(cfg.Commands[0].Args) != 2 || cfg.Commands[0].Args[0] != "--all" || cfg.Commands[0].Args[1] != "--force" {
		t.Errorf("Commands[0].Args = %v, want [--all --force]", cfg.Commands[0].Args)
	}
	if cfg.Commands[0].Shell != "" {
		t.Errorf("Commands[0].Shell = %q, want empty", cfg.Commands[0].Shell)
	}
	if cfg.Commands[1].Shell != "netsh advfirewall set allprofiles state off" {
		t.Errorf("Commands[1].Shell = %q", cfg.Commands[1].Shell)
	}
	if cfg.Commands[1].Exec != "" {
		t.Errorf("Commands[1].Exec = %q, want empty", cfg.Commands[1].Exec)
	}
}

func TestApplyDefaultsMode(t *testing.T) {
	cfg := &Config{}
	cfg.ApplyDefaults()
	if cfg.Mode != "install" {
		t.Errorf("default Mode = %q, want %q", cfg.Mode, "install")
	}

	cfg2 := &Config{Mode: "run"}
	cfg2.ApplyDefaults()
	if cfg2.Mode != "run" {
		t.Errorf("Mode = %q, want %q", cfg2.Mode, "run")
	}
}

// writeRunConfig writes a mode="run" config (plus a real bin/cleanup item) into a
// temp dir, appending extra TOML body. Returns parsed config and the dir.
func writeRunConfig(t *testing.T, body string) (*Config, string) {
	t.Helper()
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "cleanup"), []byte("exe"), 0755); err != nil {
		t.Fatal(err)
	}
	full := "name = \"cleanup\"\nversion = \"v1.0.0\"\nmode = \"run\"\n" + body
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(full), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseConfig(path)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ApplyDefaults()
	return cfg, dir
}

func TestValidateRunModeValid(t *testing.T) {
	cfg, dir := writeRunConfig(t, `
items = ["bin/cleanup"]

[[commands]]
exec = "bin/cleanup"
args = ["--all"]

[[commands]]
shell = "Write-Host hello"
`)
	if err := cfg.Validate(dir); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRunModeShellOnlyNoItems(t *testing.T) {
	cfg, dir := writeRunConfig(t, `
[[commands]]
shell = "Write-Host hello"
`)
	if err := cfg.Validate(dir); err != nil {
		t.Fatalf("Validate (shell-only, no items): %v", err)
	}
}

func TestValidateRunModeRejectsServicesDirsOverlay(t *testing.T) {
	cases := []struct{ name, body string }{
		{"services", "items = [\"bin/cleanup\"]\n[[commands]]\nexec = \"bin/cleanup\"\n[[services]]\nname = \"x\"\nexec = \"bin/cleanup\"\n"},
		{"dirs", "[[commands]]\nshell = \"x\"\n[[dirs]]\npath = \"/etc/x\"\n"},
		{"overlay", "[[commands]]\nshell = \"x\"\n[[system_overlay]]\nsystem = \"/etc/x\"\nitem = \"bin/cleanup\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, dir := writeRunConfig(t, tc.body)
			if err := cfg.Validate(dir); err == nil {
				t.Errorf("Validate should reject %s in run mode", tc.name)
			}
		})
	}
}

func TestValidateRunModeRequiresCommands(t *testing.T) {
	cfg, dir := writeRunConfig(t, `items = ["bin/cleanup"]`+"\n")
	if err := cfg.Validate(dir); err == nil {
		t.Error("Validate should reject run mode with no commands")
	}
}

func TestValidateRunModeCommandShape(t *testing.T) {
	cases := []struct{ name, body string }{
		{"both", "items = [\"bin/cleanup\"]\n[[commands]]\nexec = \"bin/cleanup\"\nshell = \"x\"\n"},
		{"neither", "[[commands]]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, dir := writeRunConfig(t, tc.body)
			if err := cfg.Validate(dir); err == nil {
				t.Errorf("Validate should reject command with %s set", tc.name)
			}
		})
	}
}

func TestValidateRunModeExecMustMatchItem(t *testing.T) {
	// exec references a file that is not a bundled item
	cfg, dir := writeRunConfig(t, `
items = ["bin/cleanup"]

[[commands]]
exec = "bin/other"
`)
	if err := cfg.Validate(dir); err == nil {
		t.Error("Validate should reject exec that does not match an item")
	}
}

func TestValidateRunModeExecRejectsPlatformMarker(t *testing.T) {
	cfg, dir := writeRunConfig(t, `
items = ["bin/cleanup.{platform}"]

[[commands]]
exec = "bin/cleanup.{platform}"
`)
	if err := cfg.Validate(dir); err == nil {
		t.Error("Validate should reject {platform} marker in command exec (use stripped form)")
	}
}

func TestValidateRunModeExecStrippedPlatformMatches(t *testing.T) {
	cfg, dir := writeRunConfig(t, `
items = ["bin/cleanup.{platform}"]

[[commands]]
exec = "bin/cleanup"
`)
	if err := cfg.Validate(dir); err != nil {
		t.Errorf("stripped-form exec should match a {platform} item: %v", err)
	}
}

func TestValidateInstallModeRejectsCommands(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "app"), []byte("exe"), 0755)
	full := `
name = "app"
version = "v1.0.0"
mode = "install"
items = ["bin/app"]

[[commands]]
shell = "Write-Host hi"
`
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(full), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(dir); err == nil {
		t.Error("Validate should reject [[commands]] in install mode")
	}
}

func TestValidateBadMode(t *testing.T) {
	dir := t.TempDir()
	full := "name = \"x\"\nversion = \"v1\"\nmode = \"bogus\"\nitems = [\"bin/x\"]\n"
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(full), 0644)
	cfg, err := ParseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(dir); err == nil {
		t.Error("Validate should reject unknown mode")
	}
}

func TestValidateKnownFolderInDirsAndOverlay(t *testing.T) {
	dir := t.TempDir()
	// Real items so install-mode item / overlay-item checks pass.
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "app"), []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "default.conf"), []byte("k=v"), 0644); err != nil {
		t.Fatal(err)
	}
	content := `
name = "kf"
version = "v1.0.0"
items = ["bin/app", "default.conf"]

[[dirs]]
path = "{AppDataFolder}\\MyApp\\data"
mode = "0755"

[[system_overlay]]
system = "{ProgramFiles64Folder}\\OtherApp\\config.json"
item = "default.conf"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseConfig(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(dir); err != nil {
		t.Fatalf("Validate should accept known-folder markers: %v", err)
	}
}

func TestValidateRejectsUnknownFolderMarker(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "app"), []byte("x"), 0755)

	content := `
name = "kf"
version = "v1.0.0"
items = ["bin/app"]

[[dirs]]
path = "{AppData}\\MyApp"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseConfig(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	err = cfg.Validate(dir)
	if err == nil {
		t.Fatal("Validate should reject unknown marker {AppData}")
	}
	if !strings.Contains(err.Error(), "AppData") {
		t.Errorf("error should name the unknown variable, got: %v", err)
	}
}

func TestValidateOverlayUnmarkedStillRequiresAbs(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0755)
	os.WriteFile(filepath.Join(dir, "bin", "app"), []byte("x"), 0755)
	content := `
name = "kf"
version = "v1.0.0"
items = ["bin/app"]

[[system_overlay]]
system = "relative/path"
item = "bin/app"
`
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0644)
	cfg, err := ParseConfig(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(dir); err == nil {
		t.Error("unmarked non-absolute overlay system should still be rejected")
	}
}

func TestValidateRunArgsKnownFolder(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "app.exe"), []byte("x"), 0755)
	content := `
name = "kf"
version = "v1.0.0"
mode = "run"
items = ["bin/app.exe"]

[[commands]]
exec = "bin/app.exe"
args = ["--config", "{AppDataFolder}\\MyApp\\config.json"]
`
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0644)
	cfg, err := ParseConfig(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(dir); err != nil {
		t.Fatalf("Validate should accept known marker in run args: %v", err)
	}
}

func TestValidateRunArgsRejectsUnknownFolder(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "app.exe"), []byte("x"), 0755)
	content := `
name = "kf"
version = "v1.0.0"
mode = "run"
items = ["bin/app.exe"]

[[commands]]
exec = "bin/app.exe"
args = ["{BogusFolder}\\x"]
`
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0644)
	cfg, err := ParseConfig(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(dir); err == nil {
		t.Error("Validate should reject unknown marker in run args")
	}
}
