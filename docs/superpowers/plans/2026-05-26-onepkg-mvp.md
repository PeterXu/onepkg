# onepkg MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a general-purpose self-extracting installer generator (`onepkg`) that reads `config.toml` and produces platform-specific `.sh` packages for Linux/macOS with systemd/launchd service support.

**Architecture:** CLI tool parses `config.toml`, collects files with SHA1 checksums, generates a VERSION file, creates systemd/launchd service configs, bundles everything into a tar.gz payload, and wraps it in a self-extracting shell script. The generated script supports install, uninstall, version, list, and verify commands.

**Tech Stack:** Go 1.21, `github.com/BurntSushi/toml`, standard library only for everything else.

---

## File Structure

All new code lives under `cmd/onepkg/` as a standalone Go module. The existing `build-package/` is untouched.

```
cmd/onepkg/
├── go.mod
├── go.sum
├── main.go              # CLI entry point, argument parsing, orchestration
├── config.go            # TOML parsing, defaults, validation
├── config_test.go
├── packager.go          # File collection, SHA1, tar.gz building
├── packager_test.go
├── version.go           # VERSION file generation
├── version_test.go
└── installer/
    ├── shell.go         # Self-extracting .sh generator
    ├── shell_test.go
    ├── systemd.go       # systemd unit file generator
    ├── systemd_test.go
    ├── launchd.go       # launchd plist generator
    └── launchd_test.go
```

**Package layout:**
- `cmd/onepkg/*.go` (excluding `installer/`) → `package main`
- `cmd/onepkg/installer/*.go` → `package installer`, imported as `onepkg/installer`

---

### Task 1: Project Scaffold

**Files:**
- Create: `cmd/onepkg/go.mod`
- Create: `cmd/onepkg/installer/` directory

- [ ] **Step 1: Create directory structure**

```bash
mkdir -p cmd/onepkg/installer
```

- [ ] **Step 2: Initialize Go module and add dependency**

```bash
cd cmd/onepkg && go mod init onepkg && go get github.com/BurntSushi/toml
```

- [ ] **Step 3: Verify module builds**

Run: `cd cmd/onepkg && go build ./...`
Expected: success (no errors, no output)

- [ ] **Step 4: Commit**

```bash
git add cmd/onepkg/go.mod cmd/onepkg/go.sum
git commit -m "feat(onepkg): initialize Go module with TOML dependency"
```

---

### Task 2: Config Parsing

**Files:**
- Create: `cmd/onepkg/config.go`
- Create: `cmd/onepkg/config_test.go`

- [ ] **Step 1: Write the failing test**

`cmd/onepkg/config_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
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
		t.Errorf("len(Platforms) = %d, want 0 (applied as default later)", len(cfg.Platforms))
	}
}

func TestParseConfigFileNotFound(t *testing.T) {
	_, err := ParseConfig("/nonexistent/config.toml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestParseConfig -v`
Expected: FAIL — `undefined: ParseConfig`

- [ ] **Step 3: Write minimal implementation**

`cmd/onepkg/config.go`:

```go
package main

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Name      string          `toml:"name"`
	Version   string          `toml:"version"`
	Platforms []string        `toml:"platforms"`
	OutputDir string          `toml:"output_dir"`
	Items     []string        `toml:"items"`
	Services  []ServiceConfig `toml:"services"`
}

type ServiceConfig struct {
	Name    string       `toml:"name"`
	Exec    string       `toml:"exec"`
	Restart string       `toml:"restart"`
	User    string       `toml:"user"`
	Linux   LinuxService `toml:"linux"`
	MacOS   MacOSService `toml:"macos"`
}

type LinuxService struct {
	Type       string   `toml:"type"`
	After      []string `toml:"after"`
	RestartSec int      `toml:"restart_sec"`
}

type MacOSService struct {
	RunAtLoad bool `toml:"run_at_load"`
	KeepAlive bool `toml:"keep_alive"`
}

func ParseConfig(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run TestParseConfig -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/config.go cmd/onepkg/config_test.go
git commit -m "feat(onepkg): add TOML config parsing with types"
```

---

### Task 3: Config Defaults and Validation

**Files:**
- Modify: `cmd/onepkg/config.go`
- Modify: `cmd/onepkg/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onepkg/config_test.go`:

```go
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
	if len(cfg.Platforms) != 1 {
		t.Fatalf("len(Platforms) = %d, want 1", len(cfg.Platforms))
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
tt{
tttname:    "path traversal prefix",
tttcfg:     &Config{Name: "myapp", Version: "v1.0.0", Items: []string{"../../../etc/passwd"}},
tttwantErr: "path traversal",
tt},
tt{
tttname:    "path traversal mid path",
tttcfg:     &Config{Name: "myapp", Version: "v1.0.0", Items: []string{"foo/../../etc/passwd"}},
tttwantErr: "path traversal",
tt},
tt{
tttname:    "dotdot in filename allowed",
tttcfg:     &Config{Name: "myapp", Version: "v1.0.0", Items: []string{"bin/my..app"}},
tttwantErr: "does not exist",
tt},
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
```

Add `"strings"` to the imports in `config_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run "TestApplyDefaults|TestValidate" -v`
Expected: FAIL — `cfg.ApplyDefaults undefined`, `cfg.Validate undefined`

- [ ] **Step 3: Write implementation**

Replace `cmd/onepkg/config.go` with the complete file:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Name      string          `toml:"name"`
	Version   string          `toml:"version"`
	Platforms []string        `toml:"platforms"`
	OutputDir string          `toml:"output_dir"`
	Items     []string        `toml:"items"`
	Services  []ServiceConfig `toml:"services"`
}

type ServiceConfig struct {
	Name    string       `toml:"name"`
	Exec    string       `toml:"exec"`
	Restart string       `toml:"restart"`
	User    string       `toml:"user"`
	Linux   LinuxService `toml:"linux"`
	MacOS   MacOSService `toml:"macos"`
}

type LinuxService struct {
	Type       string   `toml:"type"`
	After      []string `toml:"after"`
	RestartSec int      `toml:"restart_sec"`
}

type MacOSService struct {
	RunAtLoad bool `toml:"run_at_load"`
	KeepAlive bool `toml:"keep_alive"`
}

func ParseConfig(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

func (c *Config) ApplyDefaults() {
	if c.OutputDir == "" {
		c.OutputDir = "packages"
	}
	if len(c.Platforms) == 0 {
		c.Platforms = []string{runtime.GOOS + "-" + runtime.GOARCH}
	}
	for i := range c.Services {
		if c.Services[i].Name == "" {
			c.Services[i].Name = c.Name
		}
		if c.Services[i].Restart == "" {
			c.Services[i].Restart = "on-failure"
		}
		if c.Services[i].User == "" {
			c.Services[i].User = "root"
		}
		if c.Services[i].Exec == "" {
			for _, item := range c.Items {
				if strings.HasPrefix(item, "bin/") {
					c.Services[i].Exec = item
					break
				}
			}
		}
	}
}

func (c *Config) Validate(configDir string) error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if c.Version == "" {
		return fmt.Errorf("version is required")
	}
	if len(c.Items) == 0 {
		return fmt.Errorf("at least one item is required")
	}
	for _, item := range c.Items {
		if strings.Contains(item, "/..") || strings.HasPrefix(item, "..") {
			return fmt.Errorf("path traversal not allowed in item %q", item)
		}
		if filepath.IsAbs(item) {
			return fmt.Errorf("absolute path not allowed in item %q", item)
		}
		fullPath := filepath.Join(configDir, item)
		if _, err := os.Stat(fullPath); err != nil {
			return fmt.Errorf("item %q does not exist", item)
		}
	}
	itemSet := make(map[string]bool, len(c.Items))
	for _, item := range c.Items {
		itemSet[item] = true
	}
	for _, svc := range c.Services {
		if svc.Exec != "" && !itemSet[svc.Exec] {
			matched := false
			for _, item := range c.Items {
				if strings.HasPrefix(svc.Exec, item) {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("service exec %q not found in items", svc.Exec)
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run "TestApplyDefaults|TestValidate" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/config.go cmd/onepkg/config_test.go
git commit -m "feat(onepkg): add config defaults and validation"
```

---

### Task 4: File Collection with SHA1

**Files:**
- Create: `cmd/onepkg/packager.go`
- Create: `cmd/onepkg/packager_test.go`

- [ ] **Step 1: Write the failing test**

`cmd/onepkg/packager_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectFiles(t *testing.T) {
	dir := t.TempDir()

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	etcDir := filepath.Join(dir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatal(err)
	}

	bin1 := filepath.Join(binDir, "test1")
	if err := os.WriteFile(bin1, []byte("binary1"), 0755); err != nil {
		t.Fatal(err)
	}
	bin2 := filepath.Join(binDir, "test2")
	if err := os.WriteFile(bin2, []byte("binary2"), 0755); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(etcDir, "gate.conf")
	if err := os.WriteFile(conf, []byte("config-data"), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := CollectFiles(dir, []string{"bin/test1", "bin/test2", "etc/gate.conf"})
	if err != nil {
		t.Fatalf("CollectFiles returned error: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}

	pathMap := make(map[string]*FileEntry)
	for i := range entries {
		pathMap[entries[i].RelPath] = &entries[i]
	}

	if _, ok := pathMap["bin/test1"]; !ok {
		t.Error("missing bin/test1")
	}
	if _, ok := pathMap["bin/test2"]; !ok {
		t.Error("missing bin/test2")
	}
	if _, ok := pathMap["etc/gate.conf"]; !ok {
		t.Error("missing etc/gate.conf")
	}

	for _, e := range entries {
		if e.SHA1 == "" {
			t.Errorf("SHA1 empty for %s", e.RelPath)
		}
		if e.Mode == 0 {
			t.Errorf("Mode not set for %s", e.RelPath)
		}
	}
}

func TestCollectFilesDirectory(t *testing.T) {
	dir := t.TempDir()

	resDir := filepath.Join(dir, "res")
	if err := os.MkdirAll(resDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resDir, "a.png"), []byte("img-a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resDir, "b.png"), []byte("img-b"), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := CollectFiles(dir, []string{"res/"})
	if err != nil {
		t.Fatalf("CollectFiles returned error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	pathMap := make(map[string]string)
	for _, e := range entries {
		pathMap[e.RelPath] = e.SHA1
	}

	if _, ok := pathMap["res/a.png"]; !ok {
		t.Error("missing res/a.png")
	}
	if _, ok := pathMap["res/b.png"]; !ok {
		t.Error("missing res/b.png")
	}
}

func TestCollectFilesPreservesPermissions(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "run"), []byte("exec"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "data"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := CollectFiles(dir, []string{"bin/run", "bin/data"})
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if e.RelPath == "bin/run" {
			if e.Mode&0111 == 0 {
				t.Errorf("bin/run mode = %o, expected execute bit set", e.Mode)
			}
		}
		if e.RelPath == "bin/data" {
			if e.Mode&0111 != 0 {
				t.Errorf("bin/data mode = %o, expected no execute bit", e.Mode)
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestCollectFiles -v`
Expected: FAIL — `undefined: CollectFiles`

- [ ] **Step 3: Write implementation**

`cmd/onepkg/packager.go`:

```go
package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type FileEntry struct {
	RelPath string
	AbsPath string
	SHA1    string
	Mode    fs.FileMode
	Size    int64
}

func CollectFiles(configDir string, items []string) ([]FileEntry, error) {
	var entries []FileEntry
	for _, item := range items {
		cleanItem := strings.TrimRight(item, "/")
		fullPath := filepath.Join(configDir, cleanItem)

		info, err := os.Stat(fullPath)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", item, err)
		}

		if info.IsDir() {
			err = filepath.WalkDir(fullPath, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() {
					return nil
				}
				relPath, err := filepath.Rel(configDir, path)
				if err != nil {
					return err
				}
				entry, err := makeFileEntry(configDir, relPath)
				if err != nil {
					return err
				}
				entries = append(entries, entry)
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("walk %s: %w", item, err)
			}
		} else {
			entry, err := makeFileEntry(configDir, cleanItem)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func makeFileEntry(configDir, relPath string) (FileEntry, error) {
	absPath := filepath.Join(configDir, relPath)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return FileEntry{}, fmt.Errorf("read %s: %w", relPath, err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return FileEntry{}, fmt.Errorf("stat %s: %w", relPath, err)
	}
	hash := sha1.Sum(data)
	return FileEntry{
		RelPath: relPath,
		AbsPath: absPath,
		SHA1:    hex.EncodeToString(hash[:]),
		Mode:    info.Mode(),
		Size:    info.Size(),
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run TestCollectFiles -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/packager.go cmd/onepkg/packager_test.go
git commit -m "feat(onepkg): add file collection with SHA1 checksums"
```

---

### Task 5: Tar.gz Archive Building

**Files:**
- Modify: `cmd/onepkg/packager.go`
- Modify: `cmd/onepkg/packager_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/onepkg/packager_test.go`:

```go
func TestBuildTarGZ(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "app"), []byte("hello"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "etc", "app.conf"), []byte("key=val"), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := CollectFiles(dir, []string{"bin/app", "etc/app.conf"})
	if err != nil {
		t.Fatal(err)
	}

	tarData, err := BuildTarGZ(entries)
	if err != nil {
		t.Fatalf("BuildTarGZ returned error: %v", err)
	}

	gzReader, err := gzip.NewReader(bytes.NewReader(tarData))
	if err != nil {
		t.Fatalf("gzip reader error: %v", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	found := map[string]bool{}
	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read error: %v", err)
		}
		found[hdr.Name] = true

		if hdr.Name == "bin/app" {
			if hdr.Mode&0111 == 0 {
				t.Errorf("bin/app mode in tar = %o, expected execute bit", hdr.Mode)
			}
		}
	}

	if !found["bin/app"] {
		t.Error("tar missing bin/app")
	}
	if !found["etc/app.conf"] {
		t.Error("tar missing etc/app.conf")
	}
}
```

Add `"archive/tar"`, `"bytes"`, `"compress/gzip"`, `"io"` to imports in `packager_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestBuildTarGZ -v`
Expected: FAIL — `undefined: BuildTarGZ`

- [ ] **Step 3: Write implementation**

Replace `cmd/onepkg/packager.go` with the complete file:

```go
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type FileEntry struct {
	RelPath string
	AbsPath string
	SHA1    string
	Mode    fs.FileMode
	Size    int64
}

func CollectFiles(configDir string, items []string) ([]FileEntry, error) {
	var entries []FileEntry
	for _, item := range items {
		cleanItem := strings.TrimRight(item, "/")
		fullPath := filepath.Join(configDir, cleanItem)

		info, err := os.Stat(fullPath)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", item, err)
		}

		if info.IsDir() {
			err = filepath.WalkDir(fullPath, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() {
					return nil
				}
				relPath, err := filepath.Rel(configDir, path)
				if err != nil {
					return err
				}
				entry, err := makeFileEntry(configDir, relPath)
				if err != nil {
					return err
				}
				entries = append(entries, entry)
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("walk %s: %w", item, err)
			}
		} else {
			entry, err := makeFileEntry(configDir, cleanItem)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func makeFileEntry(configDir, relPath string) (FileEntry, error) {
	absPath := filepath.Join(configDir, relPath)
	data, err := os.ReadFile(absPath)
	if err != nil {
		return FileEntry{}, fmt.Errorf("read %s: %w", relPath, err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return FileEntry{}, fmt.Errorf("stat %s: %w", relPath, err)
	}
	hash := sha1.Sum(data)
	return FileEntry{
		RelPath: relPath,
		AbsPath: absPath,
		SHA1:    hex.EncodeToString(hash[:]),
		Mode:    info.Mode(),
		Size:    info.Size(),
	}, nil
}

func BuildTarGZ(entries []FileEntry) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for _, entry := range entries {
		data, err := os.ReadFile(entry.AbsPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.RelPath, err)
		}

		hdr := &tar.Header{
			Name: entry.RelPath,
			Mode: int64(entry.Mode),
			Size: entry.Size,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("tar header %s: %w", entry.RelPath, err)
		}
		if _, err := tw.Write(data); err != nil {
			return nil, fmt.Errorf("tar write %s: %w", entry.RelPath, err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("close gzip: %w", err)
	}

	return buf.Bytes(), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run TestBuildTarGZ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/packager.go cmd/onepkg/packager_test.go
git commit -m "feat(onepkg): add tar.gz archive building"
```

---

### Task 6: VERSION File Generation

**Files:**
- Create: `cmd/onepkg/version.go`
- Create: `cmd/onepkg/version_test.go`

- [ ] **Step 1: Write the failing test**

`cmd/onepkg/version_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestGenerateVersion -v`
Expected: FAIL — `undefined: VersionInfo`, `undefined: GenerateVersion`

- [ ] **Step 3: Write implementation**

`cmd/onepkg/version.go`:

```go
package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type VersionInfo struct {
	Name     string
	Version  string
	Platform string
	Created  time.Time
	Files    int
	SHA      map[string]string
}

func GenerateVersion(info VersionInfo) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "name = %q\n", info.Name)
	fmt.Fprintf(&b, "version = %q\n", info.Version)
	fmt.Fprintf(&b, "platform = %q\n", info.Platform)
	fmt.Fprintf(&b, "created = %q\n", info.Created.Format(time.RFC3339))
	fmt.Fprintf(&b, "files = %d\n", info.Files)
	b.WriteString("\n[sha]\n")

	paths := make([]string, 0, len(info.SHA))
	for p := range info.SHA {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		fmt.Fprintf(&b, "%q = %q\n", p, info.SHA[p])
	}

	return b.String(), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run TestGenerateVersion -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/version.go cmd/onepkg/version_test.go
git commit -m "feat(onepkg): add VERSION file generation"
```

---

### Task 7: systemd Unit Generation

**Files:**
- Create: `cmd/onepkg/installer/systemd.go`
- Create: `cmd/onepkg/installer/systemd_test.go`

- [ ] **Step 1: Write the failing test**

`cmd/onepkg/installer/systemd_test.go`:

```go
package installer

import (
	"strings"
	"testing"
)

func TestGenerateSystemdUnit(t *testing.T) {
	cfg := SystemdConfig{
		Name:       "autotest",
		Exec:       "/usr/local/autotest/bin/test1",
		Restart:    "on-failure",
		User:       "autotest",
		Type:       "simple",
		After:      []string{"network.target"},
		RestartSec: 5,
	}

	output := GenerateSystemdUnit(cfg)

	tests := []struct{ substr string }{
		{`[Unit]`},
		{`Description=autotest`},
		{`After=network.target`},
		{`[Service]`},
		{`Type=simple`},
		{`ExecStart=/usr/local/autotest/bin/test1`},
		{`Restart=on-failure`},
		{`RestartSec=5`},
		{`User=autotest`},
		{`[Install]`},
		{`WantedBy=multi-user.target`},
	}

	for _, tt := range tests {
		if !strings.Contains(output, tt.substr) {
			t.Errorf("output missing %q\nfull output:\n%s", tt.substr, output)
		}
	}
}

func TestGenerateSystemdUnitMinimal(t *testing.T) {
	cfg := SystemdConfig{
		Name:    "myapp",
		Exec:    "/usr/local/myapp/bin/myapp",
		Restart: "always",
		User:    "root",
	}

	output := GenerateSystemdUnit(cfg)

	if strings.Contains(output, "After=") {
		t.Error("minimal config should not contain After=")
	}
	if strings.Contains(output, "RestartSec=") {
		t.Error("minimal config should not contain RestartSec=")
	}
	if !strings.Contains(output, "Restart=always") {
		t.Error("missing Restart=always")
	}
	if !strings.Contains(output, "User=root") {
		t.Error("missing User=root")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test ./installer/ -run TestGenerateSystemdUnit -v`
Expected: FAIL — `undefined: SystemdConfig`, `undefined: GenerateSystemdUnit`

- [ ] **Step 3: Write implementation**

`cmd/onepkg/installer/systemd.go`:

```go
package installer

import (
	"fmt"
	"strings"
)

type SystemdConfig struct {
	Name       string
	Exec       string
	Restart    string
	User       string
	Type       string
	After      []string
	RestartSec int
}

func GenerateSystemdUnit(cfg SystemdConfig) string {
	var b strings.Builder

	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=%s\n", cfg.Name)
	if len(cfg.After) > 0 {
		fmt.Fprintf(&b, "After=%s\n", strings.Join(cfg.After, " "))
	}
	b.WriteString("\n")

	b.WriteString("[Service]\n")
	if cfg.Type != "" {
		fmt.Fprintf(&b, "Type=%s\n", cfg.Type)
	}
	fmt.Fprintf(&b, "ExecStart=%s\n", cfg.Exec)
	fmt.Fprintf(&b, "Restart=%s\n", cfg.Restart)
	if cfg.RestartSec > 0 {
		fmt.Fprintf(&b, "RestartSec=%d\n", cfg.RestartSec)
	}
	fmt.Fprintf(&b, "User=%s\n", cfg.User)
	b.WriteString("\n")

	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")

	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test ./installer/ -run TestGenerateSystemdUnit -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/installer/systemd.go cmd/onepkg/installer/systemd_test.go
git commit -m "feat(onepkg): add systemd unit file generation"
```

---

### Task 8: launchd Plist Generation

**Files:**
- Create: `cmd/onepkg/installer/launchd.go`
- Create: `cmd/onepkg/installer/launchd_test.go`

- [ ] **Step 1: Write the failing test**

`cmd/onepkg/installer/launchd_test.go`:

```go
package installer

import (
	"strings"
	"testing"
)

func TestGenerateLaunchdPlist(t *testing.T) {
	cfg := LaunchdConfig{
		Name:      "autotest",
		Exec:      "/opt/autotest/bin/test1",
		User:      "autotest",
		RunAtLoad: true,
		KeepAlive: true,
	}

	output := GenerateLaunchdPlist(cfg)

	tests := []struct{ substr string }{
		{`<?xml version="1.0" encoding="UTF-8"?>`},
		{`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"`},
		{`<plist version="1.0">`},
		{`<dict>`},
		{`<key>Label</key>`},
		{`<string>com.autotest</string>`},
		{`<key>ProgramArguments</key>`},
		{`<string>/opt/autotest/bin/test1</string>`},
		{`<key>RunAtLoad</key>`},
		{`<true/>`},
		{`<key>KeepAlive</key>`},
		{`<key>UserName</key>`},
		{`<string>autotest</string>`},
		{`</dict>`},
		{`</plist>`},
	}

	for _, tt := range tests {
		if !strings.Contains(output, tt.substr) {
			t.Errorf("output missing %q\nfull output:\n%s", tt.substr, output)
		}
	}
}

func TestGenerateLaunchdPlistMinimal(t *testing.T) {
	cfg := LaunchdConfig{
		Name: "myapp",
		Exec: "/opt/myapp/bin/myapp",
		User: "root",
	}

	output := GenerateLaunchdPlist(cfg)

	if strings.Contains(output, "RunAtLoad") {
		t.Error("minimal config should not contain RunAtLoad")
	}
	if strings.Contains(output, "KeepAlive") {
		t.Error("minimal config should not contain KeepAlive")
	}
	if !strings.Contains(output, "<string>root</string>") {
		t.Error("missing UserName root")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test ./installer/ -run TestGenerateLaunchdPlist -v`
Expected: FAIL — `undefined: LaunchdConfig`, `undefined: GenerateLaunchdPlist`

- [ ] **Step 3: Write implementation**

`cmd/onepkg/installer/launchd.go`:

```go
package installer

import (
	"fmt"
	"strings"
)

type LaunchdConfig struct {
	Name      string
	Exec      string
	User      string
	RunAtLoad bool
	KeepAlive bool
}

func GenerateLaunchdPlist(cfg LaunchdConfig) string {
	var b strings.Builder

	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString("\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`)
	b.WriteString("\n")
	b.WriteString(`<plist version="1.0">`)
	b.WriteString("\n<dict>\n")

	fmt.Fprintf(&b, "\t<key>Label</key>\n")
	fmt.Fprintf(&b, "\t<string>com.%s</string>\n", cfg.Name)

	fmt.Fprintf(&b, "\t<key>ProgramArguments</key>\n")
	fmt.Fprintf(&b, "\t<array>\n")
	fmt.Fprintf(&b, "\t\t<string>%s</string>\n", cfg.Exec)
	fmt.Fprintf(&b, "\t</array>\n")

	if cfg.RunAtLoad {
		fmt.Fprintf(&b, "\t<key>RunAtLoad</key>\n")
		fmt.Fprintf(&b, "\t<true/>\n")
	}

	if cfg.KeepAlive {
		fmt.Fprintf(&b, "\t<key>KeepAlive</key>\n")
		fmt.Fprintf(&b, "\t<true/>\n")
	}

	fmt.Fprintf(&b, "\t<key>UserName</key>\n")
	fmt.Fprintf(&b, "\t<string>%s</string>\n", cfg.User)

	b.WriteString("</dict>\n")
	b.WriteString("</plist>\n")

	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test ./installer/ -run TestGenerateLaunchdPlist -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/installer/launchd.go cmd/onepkg/installer/launchd_test.go
git commit -m "feat(onepkg): add launchd plist generation"
```

---

### Task 9: Shell Installer Generation

**Files:**
- Create: `cmd/onepkg/installer/shell.go`
- Create: `cmd/onepkg/installer/shell_test.go`

- [ ] **Step 1: Write the failing test**

`cmd/onepkg/installer/shell_test.go`:

```go
package installer

import (
	"strings"
	"testing"
)

func TestGenerateShellInstaller(t *testing.T) {
	cfg := ShellConfig{
		Name:           "autotest",
		Version:        "v1.0.0",
		Platform:       "linux-arm64",
		ExpectedArch:   "aarch64",
		DefaultDir:     "/usr/local/autotest",
		PayloadBase64:  "cGF5bG9hZA==",
		VersionContent: "dmVyc2lvbg==",
		ServiceContent: "c2VydmljZQ==",
		ServiceType:    "systemd",
		ServiceName:    "autotest.service",
	}

	output, err := GenerateShellInstaller(cfg)
	if err != nil {
		t.Fatalf("GenerateShellInstaller returned error: %v", err)
	}

	tests := []struct{ substr string }{
		{`#!/bin/bash`},
		{`PKG_NAME="autotest"`},
		{`PKG_VERSION="v1.0.0"`},
		{`EXPECTED_ARCH="aarch64"`},
		{`DEFAULT_DIR="/usr/local/autotest"`},
		{`SERVICE_TYPE="systemd"`},
		{`SERVICE_NAME="autotest.service"`},
		{`PAYLOAD='cGF5bG9hZA=='`},
		{`cmd_install()`},
		{`cmd_uninstall()`},
		{`cmd_version()`},
		{`cmd_list()`},
		{`cmd_verify()`},
		{`check_arch`},
		{`check_root`},
		{`base64 -d`},
		{`tar xzf`},
		{`--dir)`},
	}

	for _, tt := range tests {
		if !strings.Contains(output, tt.substr) {
			t.Errorf("output missing %q", tt.substr)
		}
	}

	if !strings.HasPrefix(output, "#!/bin/bash") {
		t.Error("script must start with shebang")
	}
}

func TestGenerateShellInstallerNoService(t *testing.T) {
	cfg := ShellConfig{
		Name:           "myapp",
		Version:        "v0.1.0",
		Platform:       "linux-amd64",
		ExpectedArch:   "x86_64",
		DefaultDir:     "/usr/local/myapp",
		PayloadBase64:  "cGF5bG9hZA==",
		VersionContent: "dmVyc2lvbg==",
		ServiceContent: "",
		ServiceType:    "none",
		ServiceName:    "",
	}

	output, err := GenerateShellInstaller(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(output, `SERVICE_TYPE="none"`) {
		t.Error("should set SERVICE_TYPE to none")
	}
	if !strings.Contains(output, `SERVICE_NAME=""`) {
		t.Error("should set empty SERVICE_NAME")
	}
}

func TestGenerateShellInstallerDarwin(t *testing.T) {
	cfg := ShellConfig{
		Name:           "autotest",
		Version:        "v1.0.0",
		Platform:       "darwin-arm64",
		ExpectedArch:   "arm64",
		DefaultDir:     "/opt/autotest",
		PayloadBase64:  "cGF5bG9hZA==",
		VersionContent: "dmVyc2lvbg==",
		ServiceContent: "c2VydmljZQ==",
		ServiceType:    "launchd",
		ServiceName:    "com.autotest.plist",
	}

	output, err := GenerateShellInstaller(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(output, `DEFAULT_DIR="/opt/autotest"`) {
		t.Error("darwin should use /opt default")
	}
	if !strings.Contains(output, `EXPECTED_ARCH="arm64"`) {
		t.Error("darwin arm64 should expect arm64")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test ./installer/ -run TestGenerateShellInstaller -v`
Expected: FAIL — `undefined: ShellConfig`, `undefined: GenerateShellInstaller`

- [ ] **Step 3: Write implementation**

`cmd/onepkg/installer/shell.go`:

```go
package installer

import (
	"fmt"
	"strings"
)

type ShellConfig struct {
	Name           string
	Version        string
	Platform       string
	ExpectedArch   string
	DefaultDir     string
	PayloadBase64  string
	VersionContent string
	ServiceContent string
	ServiceType    string
	ServiceName    string
}

func GenerateShellInstaller(cfg ShellConfig) (string, error) {
	var b strings.Builder

	fmt.Fprintf(&b, "#!/bin/bash\n")
	fmt.Fprintf(&b, "# %s %s self-extracting installer for %s\n", cfg.Name, cfg.Version, cfg.Platform)
	fmt.Fprintf(&b, "set -euo pipefail\n\n")

	fmt.Fprintf(&b, "PKG_NAME=%q\n", cfg.Name)
	fmt.Fprintf(&b, "PKG_VERSION=%q\n", cfg.Version)
	fmt.Fprintf(&b, "PKG_PLATFORM=%q\n", cfg.Platform)
	fmt.Fprintf(&b, "EXPECTED_ARCH=%q\n", cfg.ExpectedArch)
	fmt.Fprintf(&b, "DEFAULT_DIR=%q\n", cfg.DefaultDir)
	fmt.Fprintf(&b, "SERVICE_TYPE=%q\n", cfg.ServiceType)
	fmt.Fprintf(&b, "SERVICE_NAME=%q\n", cfg.ServiceName)
	b.WriteString("\n")

	b.WriteString("PAYLOAD='")
	b.WriteString(cfg.PayloadBase64)
	b.WriteString("'\n")
	b.WriteString("VERSION_B64='")
	b.WriteString(cfg.VersionContent)
	b.WriteString("'\n")
	b.WriteString("SERVICE_B64='")
	b.WriteString(cfg.ServiceContent)
	b.WriteString("'\n\n")

	b.WriteString(`compute_sha1() {
    if command -v sha1sum &>/dev/null; then
        sha1sum "$1" | cut -d' ' -f1
    else
        shasum -a 1 "$1" | cut -d' ' -f1
    fi
}

check_arch() {
    local arch=$(uname -m)
    if [ "$arch" != "$EXPECTED_ARCH" ]; then
        echo "Error: This package is for $EXPECTED_ARCH, current system is $arch"
        exit 1
    fi
}

check_root() {
    if [ "$(id -u)" != "0" ]; then
        echo "Error: This operation requires root privileges (use sudo)"
        exit 1
    fi
}

install_service() {
    local install_dir="$1"
    if [ "$SERVICE_TYPE" = "systemd" ]; then
        echo "$SERVICE_B64" | base64 -d > "/etc/systemd/system/$SERVICE_NAME"
        systemctl daemon-reload
        systemctl enable "$SERVICE_NAME"
        systemctl start "$SERVICE_NAME"
    elif [ "$SERVICE_TYPE" = "launchd" ]; then
        echo "$SERVICE_B64" | base64 -d > "/Library/LaunchDaemons/$SERVICE_NAME"
        launchctl load "/Library/LaunchDaemons/$SERVICE_NAME"
    fi
}

uninstall_service() {
    if [ "$SERVICE_TYPE" = "systemd" ]; then
        systemctl stop "$SERVICE_NAME" 2>/dev/null || true
        systemctl disable "$SERVICE_NAME" 2>/dev/null || true
        rm -f "/etc/systemd/system/$SERVICE_NAME"
        systemctl daemon-reload
    elif [ "$SERVICE_TYPE" = "launchd" ]; then
        launchctl unload "/Library/LaunchDaemons/$SERVICE_NAME" 2>/dev/null || true
        rm -f "/Library/LaunchDaemons/$SERVICE_NAME"
    fi
}

`)
	b.WriteString(`cmd_install() {
    check_arch
    check_root

    local install_dir="$DEFAULT_DIR"

    while [ $# -gt 0 ]; do
        case "$1" in
            --dir) install_dir="$2"; shift 2 ;;
            *) shift ;;
        esac
    done

    if [ -d "$install_dir" ] && [ "$(ls -A "$install_dir" 2>/dev/null)" ]; then
        echo "Warning: $install_dir exists and is not empty, replacing files..."
    fi

    mkdir -p "$install_dir"

    echo "Installing $PKG_NAME $PKG_VERSION to $install_dir..."
    echo "$PAYLOAD" | base64 -d | tar xzf - -C "$install_dir"

    echo "$VERSION_B64" | base64 -d > "$install_dir/VERSION"

    if [ "$SERVICE_TYPE" != "none" ] && [ -n "$SERVICE_B64" ]; then
        echo "Installing service..."
        install_service "$install_dir"
    fi

    echo ""
    echo "Installation complete: $install_dir"
    if [ "$SERVICE_TYPE" != "none" ]; then
        echo "Service: $SERVICE_NAME"
    fi
}

cmd_uninstall() {
    check_root

    local install_dir="$DEFAULT_DIR"

    while [ $# -gt 0 ]; do
        case "$1" in
            --dir) install_dir="$2"; shift 2 ;;
            *) shift ;;
        esac
    done

    echo "Uninstalling $PKG_NAME..."

    uninstall_service

    rm -rf "$install_dir"

    echo "Uninstallation complete."
}

cmd_version() {
    local install_dir="$DEFAULT_DIR"
    if [ -f "$install_dir/VERSION" ]; then
        cat "$install_dir/VERSION"
    else
        echo "$PKG_NAME is not installed (VERSION not found at $install_dir/VERSION)"
        exit 1
    fi
}

cmd_list() {
    local install_dir="$DEFAULT_DIR"
    if [ ! -f "$install_dir/VERSION" ]; then
        echo "$PKG_NAME is not installed"
        exit 1
    fi

    echo "Installed files in $install_dir:"
    echo ""
    grep -A9999 '^\[sha\]' "$install_dir/VERSION" | while IFS= read -r line; do
        case "$line" in
            \"*\")
                file=$(echo "$line" | sed 's/"\(.*\)" = .*/\1/')
                sha=$(echo "$line" | sed 's/.*= "\(.*\)"/\1/')
                printf "  %-40s %s\n" "$file" "$sha"
                ;;
        esac
    done
}

cmd_verify() {
    local install_dir="$DEFAULT_DIR"
    if [ ! -f "$install_dir/VERSION" ]; then
        echo "$PKG_NAME is not installed"
        exit 1
    fi

    echo "Verifying $PKG_NAME $PKG_VERSION..."
    echo ""

    local fail=0
    grep -A9999 '^\[sha\]' "$install_dir/VERSION" | while IFS= read -r line; do
        case "$line" in
            \"*\")
                file=$(echo "$line" | sed 's/"\(.*\)" = .*/\1/')
                expected=$(echo "$line" | sed 's/.*= "\(.*\)"/\1/')
                if [ ! -f "$install_dir/$file" ]; then
                    echo "MISSING: $file"
                    exit 1
                fi
                actual=$(compute_sha1 "$install_dir/$file")
                if [ "$expected" = "$actual" ]; then
                    echo "OK: $file"
                else
                    echo "FAIL: $file (expected $expected, got $actual)"
                    exit 1
                fi
                ;;
        esac
    done || fail=1

    if [ "$fail" -ne 0 ]; then
        echo ""
        echo "Verification FAILED"
        exit 1
    fi

    echo ""
    echo "Verification complete."
}

`)

	b.WriteString(`COMMAND="${1:-}"
shift 2>/dev/null || true

case "$COMMAND" in
    install)   cmd_install "$@" ;;
    uninstall) cmd_uninstall "$@" ;;
    version)   cmd_version ;;
    list)      cmd_list ;;
    verify)    cmd_verify ;;
    *)
        echo "$PKG_NAME $PKG_VERSION ($PKG_PLATFORM)"
        echo ""
        echo "Usage: $0 <command> [options]"
        echo ""
        echo "Commands:"
        echo "  install [--dir <path>]  Install to default or custom directory (requires root)"
        echo "  uninstall [--dir <path>] Remove installation and service (requires root)"
        echo "  version                Show installed version"
        echo "  list                   List installed files"
        echo "  verify                 Verify file checksums"
        exit 1
        ;;
esac
`)

	return b.String(), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test ./installer/ -run TestGenerateShellInstaller -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/installer/shell.go cmd/onepkg/installer/shell_test.go
git commit -m "feat(onepkg): add self-extracting shell installer generation"
```

---

### Task 10: CLI Entry Point and Integration Test

**Files:**
- Create: `cmd/onepkg/main.go`
- Create: `cmd/onepkg/main_test.go`

- [ ] **Step 1: Write the integration test**

`cmd/onepkg/main_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPackage(t *testing.T) {
	projectDir := t.TempDir()

	binDir := filepath.Join(projectDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	etcDir := filepath.Join(projectDir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(binDir, "app"), []byte("#!/bin/sh\necho hello"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "app.conf"), []byte("key=value"), 0644); err != nil {
		t.Fatal(err)
	}

	configContent := `
name = "testapp"
version = "v1.0.0"
output_dir = "packages"
platforms = ["linux-amd64"]

items = [
  "bin/app",
  "etc/app.conf",
]

[[services]]
name = "testapp"
exec = "bin/app"
restart = "on-failure"
user = "root"

[services.linux]
type = "simple"
after = ["network.target"]
`
	configPath := filepath.Join(projectDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	entries, err := CollectFiles(projectDir, cfg.Items)
	if err != nil {
		t.Fatalf("CollectFiles: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	tarData, err := BuildTarGZ(entries)
	if err != nil {
		t.Fatalf("BuildTarGZ: %v", err)
	}

	shaMap := make(map[string]string)
	for _, e := range entries {
		shaMap[e.RelPath] = e.SHA1
	}

	platform := cfg.Platforms[0]
	versionContent, err := GenerateVersion(VersionInfo{
		Name:     cfg.Name,
		Version:  cfg.Version,
		Platform: platform,
		Files:    len(entries),
		SHA:      shaMap,
	})
	if err != nil {
		t.Fatalf("GenerateVersion: %v", err)
	}

	if !strings.Contains(versionContent, `name = "testapp"`) {
		t.Errorf("VERSION missing name, got:\n%s", versionContent)
	}
	if !strings.Contains(versionContent, `"bin/app"`) {
		t.Errorf("VERSION missing bin/app sha, got:\n%s", versionContent)
	}

	outputDir := filepath.Join(projectDir, cfg.OutputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := BuildForPlatform(cfg, platform, entries, tarData, shaMap, outputDir); err != nil {
		t.Fatalf("BuildForPlatform: %v", err)
	}

	expectedFile := filepath.Join(outputDir, "testapp_v1.0.0_linux_amd64.sh")
	if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
		t.Fatalf("expected output file %s does not exist", expectedFile)
	}

	data, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)

	if !strings.HasPrefix(script, "#!/bin/bash") {
		t.Error("installer must start with #!/bin/bash")
	}
	if !strings.Contains(script, `PKG_NAME="testapp"`) {
		t.Error("installer missing PKG_NAME")
	}
	if !strings.Contains(script, "cmd_install") {
		t.Error("installer missing cmd_install function")
	}
	if !strings.Contains(script, "cmd_verify") {
		t.Error("installer missing cmd_verify function")
	}

	if err := os.Chmod(expectedFile, 0755); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestBuildPackage -v`
Expected: FAIL — `undefined: BuildForPlatform`

- [ ] **Step 3: Write CLI entry point and build orchestration**

`cmd/onepkg/main.go`:

```go
package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"onepkg/installer"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	var configPath string
	var platformOverride string
	var outputOverride string

	for i := 1; i < os.Args; i++ {
		switch os.Args[i] {
		case "-c", "--config":
			if i+1 < len(os.Args) {
				configPath = os.Args[i+1]
				i++
			}
		case "--platform":
			if i+1 < len(os.Args) {
				platformOverride = os.Args[i+1]
				i++
			}
		case "--output":
			if i+1 < len(os.Args) {
				outputOverride = os.Args[i+1]
				i++
			}
		case "help", "-h", "--help":
			printHelp()
			os.Exit(0)
		}
	}

	if configPath == "" {
		fmt.Println("Error: -c <config.toml> is required")
		os.Exit(1)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		fmt.Printf("Error parsing config: %v\n", err)
		os.Exit(1)
	}

	cfg.ApplyDefaults()

	if platformOverride != "" {
		cfg.Platforms = strings.Split(platformOverride, ",")
	}
	if outputOverride != "" {
		cfg.OutputDir = outputOverride
	}

	configDir := filepath.Dir(configPath)
	if err := cfg.Validate(configDir); err != nil {
		fmt.Printf("Error validating config: %v\n", err)
		os.Exit(1)
	}

	entries, err := CollectFiles(configDir, cfg.Items)
	if err != nil {
		fmt.Printf("Error collecting files: %v\n", err)
		os.Exit(1)
	}

	tarData, err := BuildTarGZ(entries)
	if err != nil {
		fmt.Printf("Error building archive: %v\n", err)
		os.Exit(1)
	}

	shaMap := make(map[string]string)
	for _, e := range entries {
		shaMap[e.RelPath] = e.SHA1
	}

	outputDir := filepath.Join(configDir, cfg.OutputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Printf("Error creating output dir: %v\n", err)
		os.Exit(1)
	}

	for _, platform := range cfg.Platforms {
		if err := BuildForPlatform(cfg, platform, entries, tarData, shaMap, outputDir); err != nil {
			fmt.Printf("Error building %s: %v\n", platform, err)
			os.Exit(1)
		}
		fmt.Printf("Created: %s\n", platformOutputPath(cfg, platform, outputDir))
	}

	fmt.Printf("\nBuilt %d platform(s)\n", len(cfg.Platforms))
}

func BuildForPlatform(cfg *Config, platform string, entries []FileEntry, tarData []byte, shaMap map[string]string, outputDir string) error {
	parts := strings.SplitN(platform, "-", 2)
	goos := parts[0]
	arch := parts[1]

	versionContent, err := GenerateVersion(VersionInfo{
		Name:     cfg.Name,
		Version:  cfg.Version,
		Platform: platform,
		Created:  time.Now().UTC(),
		Files:    len(entries),
		SHA:      shaMap,
	})
	if err != nil {
		return fmt.Errorf("generate version: %w", err)
	}

	var serviceContent string
	var serviceType string
	var serviceName string
	var defaultDir string

	switch goos {
	case "linux":
		defaultDir = "/usr/local/" + cfg.Name
		if len(cfg.Services) > 0 {
			svc := cfg.Services[0]
			serviceType = "systemd"
			serviceName = svc.Name + ".service"
			serviceContent = installer.GenerateSystemdUnit(installer.SystemdConfig{
				Name:       svc.Name,
				Exec:       filepath.Join(defaultDir, svc.Exec),
				Restart:    svc.Restart,
				User:       svc.User,
				Type:       svc.Linux.Type,
				After:      svc.Linux.After,
				RestartSec: svc.Linux.RestartSec,
			})
		}
	case "darwin":
		defaultDir = "/opt/" + cfg.Name
		if len(cfg.Services) > 0 {
			svc := cfg.Services[0]
			serviceType = "launchd"
			serviceName = "com." + svc.Name + ".plist"
			serviceContent = installer.GenerateLaunchdPlist(installer.LaunchdConfig{
				Name:      svc.Name,
				Exec:      filepath.Join(defaultDir, svc.Exec),
				User:      svc.User,
				RunAtLoad: svc.MacOS.RunAtLoad,
				KeepAlive: svc.MacOS.KeepAlive,
			})
		}
	default:
		defaultDir = "/usr/local/" + cfg.Name
	}

	if serviceType == "" {
		serviceType = "none"
	}

	expectedArch := arch
	if goos == "linux" && arch == "arm64" {
		expectedArch = "aarch64"
	}

	shellCfg := installer.ShellConfig{
		Name:           cfg.Name,
		Version:        cfg.Version,
		Platform:       platform,
		ExpectedArch:   expectedArch,
		DefaultDir:     defaultDir,
		PayloadBase64:  base64.StdEncoding.EncodeToString(tarData),
		VersionContent: base64.StdEncoding.EncodeToString([]byte(versionContent)),
		ServiceContent: base64.StdEncoding.EncodeToString([]byte(serviceContent)),
		ServiceType:    serviceType,
		ServiceName:    serviceName,
	}

	script, err := installer.GenerateShellInstaller(shellCfg)
	if err != nil {
		return fmt.Errorf("generate installer: %w", err)
	}

	outputPath := platformOutputPath(cfg, platform, outputDir)
	return os.WriteFile(outputPath, []byte(script), 0755)
}

func platformOutputPath(cfg *Config, platform string, outputDir string) string {
	platformFile := strings.ReplaceAll(platform, "-", "_")
	return filepath.Join(outputDir, fmt.Sprintf("%s_%s_%s.sh", cfg.Name, cfg.Version, platformFile))
}

func printHelp() {
	fmt.Println(`onepkg - General-purpose self-extracting installer generator

Usage:
  onepkg -c config.toml [options]

Options:
  -c, --config <path>      Path to config.toml (required)
      --platform <p>       Build for specific platform (e.g., linux-arm64)
      --output <dir>       Override output directory
  -h, --help               Show this help

Examples:
  onepkg -c config.toml
  onepkg -c config.toml --platform linux-arm64
  onepkg -c config.toml --output ./dist`)
}
```

- [ ] **Step 4: Run all tests to verify they pass**

Run: `cd cmd/onepkg && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/main.go cmd/onepkg/main_test.go
git commit -m "feat(onepkg): add CLI entry point with build orchestration"
```

---

## Self-Review

### 1. Spec Coverage

| Spec Requirement | Task |
|---|---|
| Parse config.toml with name, version, items, services | Task 2 |
| Apply defaults (output_dir, platforms, service fields) | Task 3 |
| Validate (paths exist, no traversal, service exec in items) | Task 3 |
| Collect files from items (files and directories) | Task 4 |
| Compute SHA1 for each file | Task 4 |
| Build tar.gz archive | Task 5 |
| Generate VERSION file (TOML format with sha map) | Task 6 |
| Generate systemd unit from service config | Task 7 |
| Generate launchd plist from service config | Task 8 |
| Generate self-extracting .sh script | Task 9 |
| CLI: `-c`, `--platform`, `--output` flags | Task 10 |
| Output naming: `{name}_{version}_{os}_{arch}.sh` | Task 10 |
| Default install paths (linux: /usr/local, macos: /opt) | Task 10 |
| Installer commands: install, uninstall, version, list, verify | Task 9 |
| Architecture check in generated script | Task 9 |
| Root check for install/uninstall | Task 9 |

### 2. Placeholder Scan

No TBD, TODO, or placeholder patterns found. All steps contain complete code.

### 3. Type Consistency

- `FileEntry` used in Tasks 4, 5, 10 — consistent fields (`RelPath`, `AbsPath`, `SHA1`, `Mode`, `Size`)
- `VersionInfo` defined in Task 6, used in Task 10 — consistent fields
- `SystemdConfig` defined in Task 7, used in Task 10 — consistent fields
- `LaunchdConfig` defined in Task 8, used in Task 10 — consistent fields
- `ShellConfig` defined in Task 9, used in Task 10 — consistent fields
- `ServiceConfig.Linux` / `ServiceConfig.MacOS` used consistently between config parsing (Task 2) and build orchestration (Task 10)
