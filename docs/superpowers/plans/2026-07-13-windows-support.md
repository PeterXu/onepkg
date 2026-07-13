# Windows Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Windows (windows-amd64, windows-arm64) self-extracting installer generation to onepkg, producing double-click `.exe` installers with Windows Service support.

**Architecture:** A pre-compiled Go stub executable is embedded in the onepkg binary. At packaging time, onepkg appends the install.ps1 script, a ZIP payload, and a JSON manifest to the stub, producing a self-extracting `.exe`. When run, the stub reads its own file, extracts the payload to temp, and invokes PowerShell to perform the installation (file extraction, directory creation, service management via `sc.exe`).

**Tech Stack:** Go 1.26, `archive/zip`, `encoding/json`, `embed` package, PowerShell 5.1+ (target), `sc.exe` for service management. No new external dependencies.

## Global Constraints

- Module name: `onepkg` (from `cmd/onepkg/go.mod`)
- TOML library: `github.com/pelletier/go-toml v1.9.5` (already in use, NOT BurntSushi despite CLAUDE.md)
- Target Windows versions: 8, 10, 11 (PowerShell 5.1+ built-in)
- Architectures: `windows-amd64` (→ `AMD64`), `windows-arm64` (→ `ARM64`)
- Default install path: `C:\Program Files\{name}`
- Service management: `sc.exe` only (not PowerShell `New-Service`/`Remove-Service`, which needs PS 7+)
- Payload format: ZIP (not tar.gz — Windows 8 lacks built-in tar)
- Stubs are pre-compiled and committed as binary files under `installer/stubs/`
- TDD: write failing test first, then implement. Every function gets a test.
- Package layout: `cmd/onepkg/*.go` is `package main`; `cmd/onepkg/installer/*.go` is `package installer`
- Generated installers must support: install, uninstall, version, list, verify commands

---

## File Structure

```
cmd/onepkg/
├── config.go              # MODIFY: add WindowsService struct, defaults, validation
├── config_test.go         # MODIFY: add Windows service tests
├── platform.go            # MODIFY: add "windows" to validOS
├── platform_test.go       # MODIFY: windows-amd64 now valid
├── packager.go            # MODIFY: add BuildZip, VirtualFile type
├── packager_test.go       # MODIFY: add ZIP tests
├── main.go                # MODIFY: add BuildForWindows, update BuildForPlatform, platformOutputPath, unameArch
├── main_test.go           # MODIFY: add Windows integration test
└── installer/
    ├── powershell.go      # CREATE: install.ps1 generator
    ├── powershell_test.go # CREATE: tests
    ├── exewrapper.go      # CREATE: append payload to stub, embed stubs
    ├── exewrapper_test.go # CREATE: tests
    ├── stubs/             # CREATE: pre-compiled stub binaries
    │   ├── wrapper-stub-amd64.exe
    │   └── wrapper-stub-arm64.exe
    └── stub-src/          # CREATE: stub source (separate module)
        ├── go.mod
        └── wrapper.go
```

---

### Task 1: Config — Add WindowsService Struct

**Files:**
- Modify: `cmd/onepkg/config.go`
- Modify: `cmd/onepkg/config_test.go`

**Interfaces:**
- Produces: `WindowsService` struct with fields `StartType string` and `RecoveryDelayMs int`; `ServiceConfig.Windows` field; defaults applied in `ApplyDefaults()`; validation in `Validate()`.

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onepkg/config_test.go`:

```go
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
	if svc.Windows.RecoveryDelayMs != 3000 {
		t.Errorf("Windows.RecoveryDelayMs = %d, want 3000", svc.Windows.RecoveryDelayMs)
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
	if svc.Windows.RecoveryDelayMs != 5000 {
		t.Errorf("Windows.RecoveryDelayMs default = %d, want 5000", svc.Windows.RecoveryDelayMs)
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test -run "TestParseConfigWindowsService|TestApplyDefaultsWindowsService|TestValidateWindowsServiceStartType" -v`
Expected: FAIL — `undefined: WindowsService`

- [ ] **Step 3: Add WindowsService struct to config.go**

In `cmd/onepkg/config.go`, add the `Windows` field to `ServiceConfig` and define the `WindowsService` struct. Find the existing `ServiceConfig` struct:

```go
type ServiceConfig struct {
	Name    string       `toml:"name"`
	Exec    string       `toml:"exec"`
	Args    []string     `toml:"args"`
	Restart string       `toml:"restart"`
	User    string       `toml:"user"`
	Linux   LinuxService `toml:"linux"`
	MacOS   MacOSService `toml:"macos"`
}
```

Replace it with:

```go
type ServiceConfig struct {
	Name    string        `toml:"name"`
	Exec    string        `toml:"exec"`
	Args    []string      `toml:"args"`
	Restart string        `toml:"restart"`
	User    string        `toml:"user"`
	Linux   LinuxService  `toml:"linux"`
	MacOS   MacOSService  `toml:"macos"`
	Windows WindowsService `toml:"windows"`
}

type WindowsService struct {
	StartType       string `toml:"start_type"`        // auto | manual | disabled
	RecoveryDelayMs int    `toml:"recovery_delay_ms"` // delay before restart on failure (ms)
}
```

- [ ] **Step 4: Add Windows defaults to ApplyDefaults**

In `cmd/onepkg/config.go`, find the service defaults block inside `ApplyDefaults()`:

```go
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
```

Replace it with:

```go
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
		// Windows service defaults
		if c.Services[i].Windows.StartType == "" {
			c.Services[i].Windows.StartType = "auto"
		}
		if c.Services[i].Windows.RecoveryDelayMs == 0 {
			c.Services[i].Windows.RecoveryDelayMs = 5000
		}
	}
```

- [ ] **Step 5: Add Windows validation to Validate**

In `cmd/onepkg/config.go`, find the service validation loop inside `Validate()` (the `for _, svc := range c.Services {` block). After the existing exec validation (the closing `}` of the `if svc.Exec != "" && !itemSet[svc.Exec] {` block), add Windows validation. The block currently ends with:

```go
			if !matched {
				return fmt.Errorf("service exec %q not found in items", svc.Exec)
			}
		}
	}
```

Replace with:

```go
			if !matched {
				return fmt.Errorf("service exec %q not found in items", svc.Exec)
			}
		}
		// Windows service validation
		if svc.Windows.StartType != "" {
			switch svc.Windows.StartType {
			case "auto", "manual", "disabled":
				// valid
			default:
				return fmt.Errorf("service windows start_type %q must be one of: auto, manual, disabled", svc.Windows.StartType)
			}
		}
		if svc.Windows.RecoveryDelayMs < 0 {
			return fmt.Errorf("service windows recovery_delay_ms must be >= 0")
		}
	}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test -run "TestParseConfigWindowsService|TestApplyDefaultsWindowsService|TestValidateWindowsServiceStartType" -v`
Expected: PASS

- [ ] **Step 7: Run full test suite to verify no regressions**

Run: `cd cmd/onepkg && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 8: Commit**

```bash
git add cmd/onepkg/config.go cmd/onepkg/config_test.go
git commit -m "feat(config): add WindowsService struct with start_type and recovery_delay_ms"
```

---

### Task 2: Platform — Add Windows to Valid OS

**Files:**
- Modify: `cmd/onepkg/platform.go`
- Modify: `cmd/onepkg/platform_test.go`
- Modify: `cmd/onepkg/main.go` (unameArch function)

**Interfaces:**
- Produces: `ValidatePlatform` accepts `windows-amd64` and `windows-arm64`; `unameArch("windows", "amd64")` returns `"AMD64"`, `unameArch("windows", "arm64")` returns `"ARM64"`.

- [ ] **Step 1: Update platform_test.go to expect windows as valid**

In `cmd/onepkg/platform_test.go`, find the test case:

```go
		{"windows-amd64", "invalid os"}, // MVP only supports linux/darwin
```

Replace it with:

```go
		{"windows-amd64", ""},            // Windows now supported
		{"windows-arm64", ""},            // Windows now supported
		{"windows-x86", "invalid arch"},  // x86 not supported
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestValidatePlatform -v`
Expected: FAIL — `windows-amd64` returns error but test expects no error

- [ ] **Step 3: Add windows to validOS in platform.go**

In `cmd/onepkg/platform.go`, find:

```go
var validOS = map[string]bool{
	"linux":  true,
	"darwin": true,
	// "windows": true, // Post-MVP
}
```

Replace with:

```go
var validOS = map[string]bool{
	"linux":   true,
	"darwin":  true,
	"windows": true,
}
```

Also update the error message in `ValidatePlatform`. Find:

```go
	if !validOS[os] {
		return fmt.Errorf("invalid os %q in platform %q (supported: linux, darwin)", os, platform)
	}
```

Replace with:

```go
	if !validOS[os] {
		return fmt.Errorf("invalid os %q in platform %q (supported: linux, darwin, windows)", os, platform)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run TestValidatePlatform -v`
Expected: PASS

- [ ] **Step 5: Write failing test for unameArch Windows mapping**

In `cmd/onepkg/main_test.go` (or create the test if a main_test.go with unameArch tests doesn't exist), add:

```go
func TestUnameArchWindows(t *testing.T) {
	tests := []struct {
		goos, arch, want string
	}{
		{"windows", "amd64", "AMD64"},
		{"windows", "arm64", "ARM64"},
		{"linux", "amd64", "x86_64"},
		{"linux", "arm64", "aarch64"},
		{"darwin", "arm64", "arm64"},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"-"+tt.arch, func(t *testing.T) {
			got := unameArch(tt.goos, tt.arch)
			if got != tt.want {
				t.Errorf("unameArch(%q, %q) = %q, want %q", tt.goos, tt.arch, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestUnameArchWindows -v`
Expected: FAIL — `unameArch("windows", "amd64")` returns `"amd64"` not `"AMD64"`

- [ ] **Step 7: Update unameArch in main.go**

In `cmd/onepkg/main.go`, find the `unameArch` function:

```go
func unameArch(goos, arch string) string {
	switch {
	case goos == "linux" && arch == "arm64":
		return "aarch64"
	case arch == "amd64":
		return "x86_64"
	case arch == "386":
		return "i686"
	case arch == "loong64":
		return "loongarch64"
	case arch == "mipsle":
		return "mipsel"
	case arch == "mips64le":
		return "mips64el"
	default:
		return arch
	}
}
```

Replace with:

```go
func unameArch(goos, arch string) string {
	switch {
	case goos == "windows" && arch == "amd64":
		return "AMD64"
	case goos == "windows" && arch == "arm64":
		return "ARM64"
	case goos == "linux" && arch == "arm64":
		return "aarch64"
	case arch == "amd64":
		return "x86_64"
	case arch == "386":
		return "i686"
	case arch == "loong64":
		return "loongarch64"
	case arch == "mipsle":
		return "mipsel"
	case arch == "mips64le":
		return "mips64el"
	default:
		return arch
	}
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test -run "TestUnameArchWindows|TestValidatePlatform" -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add cmd/onepkg/platform.go cmd/onepkg/platform_test.go cmd/onepkg/main.go cmd/onepkg/main_test.go
git commit -m "feat(platform): add windows-amd64 and windows-arm64 support"
```

---

### Task 3: Packager — Add ZIP Building

**Files:**
- Modify: `cmd/onepkg/packager.go`
- Modify: `cmd/onepkg/packager_test.go`

**Interfaces:**
- Produces: `VirtualFile` struct (`RelPath string`, `Content []byte`, `Mode fs.FileMode`); `BuildZip(entries []FileEntry, preservePermissions bool) ([]byte, error)`; `BuildZipWithExtras(entries []FileEntry, preservePermissions bool, extras []VirtualFile) ([]byte, error)`.

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onepkg/packager_test.go` (add `"archive/zip"` and `"io"` to imports if not present):

```go
func TestBuildZip(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0755)
	os.MkdirAll(filepath.Join(dir, "etc"), 0755)
	os.WriteFile(filepath.Join(dir, "bin", "app.exe"), []byte("hello"), 0755)
	os.WriteFile(filepath.Join(dir, "etc", "app.conf"), []byte("key=val"), 0644)

	entries, err := CollectFiles(dir, []string{"bin/app.exe", "etc/app.conf"})
	if err != nil {
		t.Fatal(err)
	}

	zipData, err := BuildZip(entries, true)
	if err != nil {
		t.Fatalf("BuildZip returned error: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("zip.NewReader error: %v", err)
	}

	found := map[string]bool{}
	for _, f := range zr.File {
		found[f.Name] = true
	}
	if !found["bin/app.exe"] {
		t.Error("zip missing bin/app.exe")
	}
	if !found["etc/app.conf"] {
		t.Error("zip missing etc/app.conf")
	}
}

func TestBuildZipWithExtras(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0755)
	os.WriteFile(filepath.Join(dir, "bin", "app.exe"), []byte("hello"), 0755)

	entries, err := CollectFiles(dir, []string{"bin/app.exe"})
	if err != nil {
		t.Fatal(err)
	}

	versionContent := "name = \"myapp\"\nversion = \"v1.0.0\"\n"
	zipData, err := BuildZipWithExtras(entries, true, []VirtualFile{
		{RelPath: "VERSION", Content: []byte(versionContent), Mode: 0644},
	})
	if err != nil {
		t.Fatalf("BuildZipWithExtras returned error: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("zip.NewReader error: %v", err)
	}

	found := map[string]bool{}
	for _, f := range zr.File {
		found[f.Name] = true
	}
	if !found["bin/app.exe"] {
		t.Error("zip missing bin/app.exe")
	}
	if !found["VERSION"] {
		t.Error("zip missing VERSION (virtual file)")
	}

	// Verify VERSION content
	for _, f := range zr.File {
		if f.Name == "VERSION" {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open VERSION: %v", err)
			}
			data, _ := io.ReadAll(rc)
			rc.Close()
			if string(data) != versionContent {
				t.Errorf("VERSION content = %q, want %q", string(data), versionContent)
			}
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test -run "TestBuildZip$" -v`
Expected: FAIL — `undefined: BuildZip`, `undefined: VirtualFile`

- [ ] **Step 3: Add ZIP building to packager.go**

In `cmd/onepkg/packager.go`, add `"archive/zip"` to the imports. Then append at the end of the file:

```go
// VirtualFile is an in-memory file to add to an archive (e.g., generated VERSION file)
type VirtualFile struct {
	RelPath string
	Content []byte
	Mode    fs.FileMode
}

// BuildZip creates a ZIP archive from file entries (for Windows installers)
func BuildZip(entries []FileEntry, preservePermissions bool) ([]byte, error) {
	return BuildZipWithExtras(entries, preservePermissions, nil)
}

// BuildZipWithExtras creates a ZIP archive from file entries plus in-memory virtual files
func BuildZipWithExtras(entries []FileEntry, preservePermissions bool, extras []VirtualFile) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for _, entry := range entries {
		data, err := os.ReadFile(entry.AbsPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.RelPath, err)
		}

		mode := entry.Mode
		if !preservePermissions {
			mode = normalizedMode(entry.Mode)
		}

		hdr := &zip.FileHeader{
			Name:   entry.RelPath,
			Method: zip.Deflate,
		}
		hdr.SetMode(mode)

		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return nil, fmt.Errorf("zip header %s: %w", entry.RelPath, err)
		}
		if _, err := w.Write(data); err != nil {
			return nil, fmt.Errorf("zip write %s: %w", entry.RelPath, err)
		}
	}

	for _, extra := range extras {
		hdr := &zip.FileHeader{
			Name:   extra.RelPath,
			Method: zip.Deflate,
		}
		hdr.SetMode(extra.Mode)

		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return nil, fmt.Errorf("zip header %s: %w", extra.RelPath, err)
		}
		if _, err := w.Write(extra.Content); err != nil {
			return nil, fmt.Errorf("zip write %s: %w", extra.RelPath, err)
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}

	return buf.Bytes(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test -run "TestBuildZip" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/packager.go cmd/onepkg/packager_test.go
git commit -m "feat(packager): add ZIP archive building for Windows installers"
```

---

### Task 4: PowerShell Installer Generator

**Files:**
- Create: `cmd/onepkg/installer/powershell.go`
- Create: `cmd/onepkg/installer/powershell_test.go`

**Interfaces:**
- Consumes: `DirSpec`, `OverlaySpec` (from existing `shell.go`)
- Produces: `WindowsServiceConfig` struct; `PowerShellConfig` struct; `GeneratePowerShellInstaller(cfg PowerShellConfig) (string, error)`.

- [ ] **Step 1: Write the failing tests**

Create `cmd/onepkg/installer/powershell_test.go`:

```go
package installer

import (
	"strings"
	"testing"
)

func TestGeneratePowerShellInstaller(t *testing.T) {
	cfg := PowerShellConfig{
		Name:         "myapp",
		Version:      "v1.0.0",
		Platform:     "windows-amd64",
		ExpectedArch: "AMD64",
		DefaultDir:   `C:\Program Files\myapp`,
		ServiceName:  "myapp",
		ServiceConfig: WindowsServiceConfig{
			StartType:       "auto",
			RecoveryDelayMs: 5000,
			Exec:            `C:\Program Files\myapp\bin\myapp.exe`,
			Args:            []string{"--config", "etc/myapp.conf"},
		},
	}

	output, err := GeneratePowerShellInstaller(cfg)
	if err != nil {
		t.Fatalf("GeneratePowerShellInstaller returned error: %v", err)
	}

	tests := []struct{ substr string }{
		{`param(`},
		{`$PKG_NAME = 'myapp'`},
		{`$PKG_VERSION = 'v1.0.0'`},
		{`$PKG_PLATFORM = 'windows-amd64'`},
		{`$EXPECTED_ARCH = 'AMD64'`},
		{`$DEFAULT_DIR = 'C:\Program Files\myapp'`},
		{`$SERVICE_NAME = 'myapp'`},
		{`$SERVICE_START_TYPE = 'auto'`},
		{`$SERVICE_RECOVERY_DELAY_MS = 5000`},
		{`$SERVICE_EXEC = 'C:\Program Files\myapp\bin\myapp.exe'`},
		{`function CheckAdmin`},
		{`function CheckArch`},
		{`function InstallService`},
		{`function UninstallService`},
		{`function CreateDirs`},
		{`function ApplyOverlays`},
		{`function Install`},
		{`function Uninstall`},
		{`sc.exe create`},
		{`sc.exe failure`},
		{`sc.exe delete`},
		{`Expand-Archive`},
		{`-Action`},
		{`switch ($Action)`},
	}

	for _, tt := range tests {
		if !strings.Contains(output, tt.substr) {
			t.Errorf("output missing %q", tt.substr)
		}
	}
}

func TestGeneratePowerShellInstallerNoService(t *testing.T) {
	cfg := PowerShellConfig{
		Name:         "myapp",
		Version:      "v1.0.0",
		Platform:     "windows-arm64",
		ExpectedArch: "ARM64",
		DefaultDir:   `C:\Program Files\myapp`,
		ServiceName:  "",
	}

	output, err := GeneratePowerShellInstaller(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(output, `$SERVICE_NAME = ''`) {
		t.Error("should set empty SERVICE_NAME when no service")
	}
	// Should not contain service install logic when no service
	if strings.Contains(output, "sc.exe create") {
		t.Error("should not contain sc.exe create when no service")
	}
}

func TestGeneratePowerShellInstallerWithDirs(t *testing.T) {
	cfg := PowerShellConfig{
		Name:         "myapp",
		Version:      "v1.0.0",
		Platform:     "windows-amd64",
		ExpectedArch: "AMD64",
		DefaultDir:   `C:\Program Files\myapp`,
		ServiceName:  "",
		Dirs: []DirSpec{
			{Path: `C:\var\log\myapp`, Owner: "myapp", Mode: "0755"},
			{Path: "data", Owner: "myapp", Mode: "0750"},
		},
	}

	output, err := GeneratePowerShellInstaller(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(output, "$DIRS_COUNT = 2") {
		t.Error("output missing DIRS_COUNT = 2")
	}
	if !strings.Contains(output, "$DIR_0_PATH = 'C:\\var\\log\\myapp'") {
		t.Error("output missing DIR_0_PATH")
	}
	if !strings.Contains(output, "$DIR_1_PATH = 'data'") {
		t.Error("output missing DIR_1_PATH")
	}
}

func TestGeneratePowerShellInstallerWithOverlays(t *testing.T) {
	cfg := PowerShellConfig{
		Name:         "myapp",
		Version:      "v1.0.0",
		Platform:     "windows-amd64",
		ExpectedArch: "AMD64",
		DefaultDir:   `C:\Program Files\myapp`,
		ServiceName:  "",
		Overlays: []OverlaySpec{
			{System: `C:\etc\agent\config.txt`, Item: "etc/config.txt"},
		},
	}

	output, err := GeneratePowerShellInstaller(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(output, "$OVERLAY_COUNT = 1") {
		t.Error("output missing OVERLAY_COUNT = 1")
	}
	if !strings.Contains(output, "$OVERLAY_0_SYSTEM = 'C:\\etc\\agent\\config.txt'") {
		t.Error("output missing OVERLAY_0_SYSTEM")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test ./installer/ -run TestGeneratePowerShellInstaller -v`
Expected: FAIL — `undefined: PowerShellConfig`, `undefined: GeneratePowerShellInstaller`

- [ ] **Step 3: Write the implementation**

Create `cmd/onepkg/installer/powershell.go`:

```go
package installer

import (
	"fmt"
	"strings"
)

// WindowsServiceConfig holds Windows service configuration for the installer
type WindowsServiceConfig struct {
	StartType       string   // auto | manual | disabled
	RecoveryDelayMs int      // delay before restart on failure (ms)
	Exec            string   // full path to executable
	Args            []string // command-line arguments
}

// PowerShellConfig configures the generated install.ps1 script
type PowerShellConfig struct {
	Name          string
	Version       string
	Platform      string
	ExpectedArch  string // AMD64 or ARM64
	DefaultDir    string // e.g., C:\Program Files\{name}
	ServiceName   string // empty if no service
	ServiceConfig WindowsServiceConfig
	Dirs          []DirSpec
	Overlays      []OverlaySpec
}

// psQuote returns a PowerShell single-quoted string literal.
// Single quotes in the value are doubled per PowerShell escaping rules.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func GeneratePowerShellInstaller(cfg PowerShellConfig) (string, error) {
	var b strings.Builder

	// Header
	fmt.Fprintf(&b, "# %s %s self-extracting installer for %s\n", cfg.Name, cfg.Version, cfg.Platform)
	b.WriteString("# Generated by onepkg\n\n")

	// Param block
	b.WriteString("param(\n")
	b.WriteString("    [string]$PayloadDir = '',\n")
	fmt.Fprintf(&b, "    [string]$InstallDir = %s,\n", psQuote(cfg.DefaultDir))
	b.WriteString("    [ValidateSet('install', 'uninstall', 'version', 'list', 'verify')]\n")
	b.WriteString("    [string]$Action = 'install'\n")
	b.WriteString(")\n\n")

	// Variables
	fmt.Fprintf(&b, "$PKG_NAME = %s\n", psQuote(cfg.Name))
	fmt.Fprintf(&b, "$PKG_VERSION = %s\n", psQuote(cfg.Version))
	fmt.Fprintf(&b, "$PKG_PLATFORM = %s\n", psQuote(cfg.Platform))
	fmt.Fprintf(&b, "$EXPECTED_ARCH = %s\n", psQuote(cfg.ExpectedArch))
	fmt.Fprintf(&b, "$DEFAULT_DIR = %s\n", psQuote(cfg.DefaultDir))
	fmt.Fprintf(&b, "$SERVICE_NAME = %s\n", psQuote(cfg.ServiceName))

	if cfg.ServiceName != "" {
		fmt.Fprintf(&b, "$SERVICE_START_TYPE = %s\n", psQuote(cfg.ServiceConfig.StartType))
		fmt.Fprintf(&b, "$SERVICE_RECOVERY_DELAY_MS = %d\n", cfg.ServiceConfig.RecoveryDelayMs)
		fmt.Fprintf(&b, "$SERVICE_EXEC = %s\n", psQuote(cfg.ServiceConfig.Exec))
		b.WriteString("$SERVICE_ARGS = @(")
		for i, arg := range cfg.ServiceConfig.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(psQuote(arg))
		}
		b.WriteString(")\n")
	}

	// Dirs
	fmt.Fprintf(&b, "$DIRS_COUNT = %d\n", len(cfg.Dirs))
	for i, d := range cfg.Dirs {
		fmt.Fprintf(&b, "$DIR_%d_PATH = %s\n", i, psQuote(d.Path))
		fmt.Fprintf(&b, "$DIR_%d_OWNER = %s\n", i, psQuote(d.Owner))
		fmt.Fprintf(&b, "$DIR_%d_MODE = %s\n", i, psQuote(d.Mode))
	}

	// Overlays
	fmt.Fprintf(&b, "$OVERLAY_COUNT = %d\n", len(cfg.Overlays))
	for i, o := range cfg.Overlays {
		fmt.Fprintf(&b, "$OVERLAY_%d_SYSTEM = %s\n", i, psQuote(o.System))
		fmt.Fprintf(&b, "$OVERLAY_%d_ITEM = %s\n", i, psQuote(o.Item))
	}

	b.WriteString("\n")

	// Functions (service functions only included when a service is defined)
	b.WriteString(powershellFunctions(cfg))

	// Main dispatch
	b.WriteString(powershellDispatch())

	return b.String(), nil
}

func powershellFunctions(cfg PowerShellConfig) string {
	hasService := cfg.ServiceName != ""
	var b strings.Builder

	b.WriteString(`function CheckAdmin {
    $currentUser = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]$currentUser
    $isAdmin = $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    if (-not $isAdmin) {
        Write-Error "This script requires administrator privileges."
        Write-Host "Run with: PowerShell -ExecutionPolicy Bypass -File ` + "`" + `"$PSCommandPath` + "`" + `" -Action install"
        exit 1
    }
}

function CheckArch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($arch -ne $EXPECTED_ARCH) {
        Write-Error "Error: This package is for $EXPECTED_ARCH, current system is $arch"
        exit 1
    }
}

`)

	if hasService {
		b.WriteString(`function InstallService {
    $binPath = "` + "`" + `"$SERVICE_EXEC` + "`" + `""
    if ($SERVICE_ARGS.Count -gt 0) {
        $binPath += " " + ($SERVICE_ARGS -join " ")
    }
    $scStartType = $SERVICE_START_TYPE
    if ($scStartType -eq 'manual') { $scStartType = 'demand' }

    Write-Host "Creating service $SERVICE_NAME..."
    sc.exe create $SERVICE_NAME binPath= $binPath start= $scStartType

    if ($SERVICE_RECOVERY_DELAY_MS -gt 0) {
        Write-Host "Configuring service recovery..."
        sc.exe failure $SERVICE_NAME actions= restart/$SERVICE_RECOVERY_DELAY_MS reset= 0
        sc.exe failureflag $SERVICE_NAME 1
    }

    if ($SERVICE_START_TYPE -eq 'auto') {
        Write-Host "Starting service..."
        sc.exe start $SERVICE_NAME
    }
}

function UninstallService {
    if ($SERVICE_NAME -eq '') { return }
    Write-Host "Stopping service $SERVICE_NAME..."
    sc.exe stop $SERVICE_NAME 2>$null
    Write-Host "Deleting service $SERVICE_NAME..."
    sc.exe delete $SERVICE_NAME
}

`)
	}

	b.WriteString(`function CreateDirs {
    if ($DIRS_COUNT -eq 0) { return }
    for ($i = 0; $i -lt $DIRS_COUNT; $i++) {
        $pathVar = "DIR_${i}_PATH"
        $dirPath = (Get-Variable -Name $pathVar -ValueOnly)
        if (-not [System.IO.Path]::IsPathRooted($dirPath)) {
            $dirPath = Join-Path $InstallDir $dirPath
        }
        New-Item -ItemType Directory -Path $dirPath -Force | Out-Null
    }
}

function ApplyOverlays {
    if ($OVERLAY_COUNT -eq 0) { return }
    for ($i = 0; $i -lt $OVERLAY_COUNT; $i++) {
        $sysVar = "OVERLAY_${i}_SYSTEM"
        $itemVar = "OVERLAY_${i}_ITEM"
        $sysPath = (Get-Variable -Name $sysVar -ValueOnly)
        $itemPath = (Get-Variable -Name $itemVar -ValueOnly)
        if (Test-Path $sysPath) {
            Write-Host "Overlay: using system $sysPath -> $itemPath"
            $destPath = Join-Path $InstallDir $itemPath
            $destParent = Split-Path -Parent $destPath
            if (-not (Test-Path $destParent)) {
                New-Item -ItemType Directory -Path $destParent -Force | Out-Null
            }
            if (Test-Path $sysPath -PathType Container) {
                Copy-Item -Path $sysPath -Destination $destPath -Recurse -Force
            } else {
                Copy-Item -Path $sysPath -Destination $destPath -Force
            }
        }
    }
}

function Install {
    CheckAdmin
    CheckArch

    if ($PayloadDir -eq '') {
        $PayloadDir = $PSScriptRoot
    }

    Write-Host "Installing $PKG_NAME $PKG_VERSION to $InstallDir..."

    if (Test-Path $InstallDir) {
        Write-Host "Warning: $InstallDir exists, replacing files..."
    }
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null

    $payloadZip = Join-Path $PayloadDir "payload.zip"
    if (Test-Path $payloadZip) {
        Expand-Archive -Path $payloadZip -DestinationPath $InstallDir -Force
    } else {
        Write-Error "payload.zip not found at $payloadZip"
        exit 1
    }

    ApplyOverlays
    CreateDirs
`)

	if hasService {
		b.WriteString(`
    Write-Host "Installing service..."
    InstallService
`)
	}

	b.WriteString(`
    Write-Host ""
    Write-Host "Installation complete: $InstallDir"
`)

	if hasService {
		b.WriteString(`    Write-Host "Service: $SERVICE_NAME"
`)
	}

	b.WriteString(`}

function Uninstall {
    CheckAdmin
    Write-Host "Uninstalling $PKG_NAME..."
`)

	if hasService {
		b.WriteString(`    UninstallService
`)
	}

	b.WriteString(`    if (Test-Path $InstallDir) {
        Remove-Item -Path $InstallDir -Recurse -Force
    }
    Write-Host "Uninstallation complete."
}

function ShowVersion {
    $versionFile = Join-Path $InstallDir "VERSION"
    if (Test-Path $versionFile) {
        Get-Content $versionFile
    } else {
        Write-Host "$PKG_NAME is not installed (VERSION not found at $versionFile)"
        exit 1
    }
}

function ListFiles {
    $versionFile = Join-Path $InstallDir "VERSION"
    if (-not (Test-Path $versionFile)) {
        Write-Host "$PKG_NAME is not installed"
        exit 1
    }
    Write-Host "Installed files in $InstallDir:"
    Get-ChildItem -Path $InstallDir -Recurse -File | ForEach-Object {
        $rel = $_.FullName.Substring($InstallDir.Length).TrimStart('\')
        Write-Host "  $rel"
    }
}

function VerifyFiles {
    $versionFile = Join-Path $InstallDir "VERSION"
    if (-not (Test-Path $versionFile)) {
        Write-Host "$PKG_NAME is not installed"
        exit 1
    }
    Write-Host "Verifying $PKG_NAME $PKG_VERSION..."
    Write-Host ""
    $fail = $false
    $content = Get-Content $versionFile -Raw
    $inSha = $false
    foreach ($line in ($content -split "` + "`" + `n")) {
        if ($line -match '^\[sha\]') { $inSha = $true; continue }
        if (-not $inSha) { continue }
        if ($line -match '^"([^"]+)"\s*=\s*"([^"]+)"') {
            $file = $matches[1]
            $expected = $matches[2]
            $fullPath = Join-Path $InstallDir $file
            if (-not (Test-Path $fullPath)) {
                Write-Host "MISSING: $file"
                $fail = $true
                continue
            }
            $actual = (Get-FileHash -Path $fullPath -Algorithm SHA1).Hash.ToLower()
            if ($actual -eq $expected.ToLower()) {
                Write-Host "OK: $file"
            } else {
                Write-Host "FAIL: $file (expected $expected, got $actual)"
                $fail = $true
            }
        }
    }
    if ($fail) {
        Write-Host ""
        Write-Host "Verification FAILED"
        exit 1
    }
    Write-Host ""
    Write-Host "Verification complete."
}

`)

	return b.String()
}

func powershellDispatch() string {
	return `switch ($Action) {
    'install'   { Install }
    'uninstall' { Uninstall }
    'version'   { ShowVersion }
    'list'      { ListFiles }
    'verify'    { VerifyFiles }
    default {
        Write-Host "$PKG_NAME $PKG_VERSION ($PKG_PLATFORM)"
        Write-Host ""
        Write-Host "Usage: $PSCommandPath -Action <command> [-PayloadDir <path>] [-InstallDir <path>]"
        Write-Host ""
        Write-Host "Commands:"
        Write-Host "  install    Install to default or custom directory"
        Write-Host "  uninstall  Remove installation and service"
        Write-Host "  version    Show installed version"
        Write-Host "  list       List installed files"
        Write-Host "  verify     Verify file checksums"
        exit 1
    }
}
`
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test ./installer/ -run TestGeneratePowerShellInstaller -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/installer/powershell.go cmd/onepkg/installer/powershell_test.go
git commit -m "feat(installer): add PowerShell installer generator for Windows"
```

---

### Task 5: Stub Source and Build

**Files:**
- Create: `cmd/onepkg/installer/stub-src/go.mod`
- Create: `cmd/onepkg/installer/stub-src/wrapper.go`
- Create: `cmd/onepkg/installer/stubs/wrapper-stub-amd64.exe` (built)
- Create: `cmd/onepkg/installer/stubs/wrapper-stub-arm64.exe` (built)

**Interfaces:**
- Produces: Two pre-compiled Windows stub executables that read appended payload (install.ps1 + payload.zip + manifest + magic marker) and run PowerShell.

- [ ] **Step 1: Create the stub module**

Create `cmd/onepkg/installer/stub-src/go.mod`:

```go
module stub

go 1.21
```

- [ ] **Step 2: Write the stub source**

Create `cmd/onepkg/installer/stub-src/wrapper.go`:

```go
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const magicMarker = "ONEPKG"

type manifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	PS1Size int64  `json:"ps1_size"`
	ZipSize int64  `json:"zip_size"`
}

func main() {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting exe path: %v\n", err)
		os.Exit(1)
	}

	exeData, err := os.ReadFile(exePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading exe: %v\n", err)
		os.Exit(1)
	}

	magicLen := len(magicMarker)
	if len(exeData) < magicLen+4 {
		fmt.Fprintf(os.Stderr, "Invalid exe format: too short\n")
		os.Exit(1)
	}

	magic := string(exeData[len(exeData)-magicLen:])
	if magic != magicMarker {
		fmt.Fprintf(os.Stderr, "Invalid exe format: no magic marker\n")
		os.Exit(1)
	}

	// Read manifest size (4 bytes before magic marker)
	manifestSize := binary.LittleEndian.Uint32(exeData[len(exeData)-magicLen-4:])
	manifestStart := len(exeData) - magicLen - 4 - int(manifestSize)

	var m manifest
	if err := json.Unmarshal(exeData[manifestStart:manifestStart+int(manifestSize)], &m); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing manifest: %v\n", err)
		os.Exit(1)
	}

	// Read payload (ps1 + zip) before manifest
	payloadStart := manifestStart - int(m.PS1Size+m.ZipSize)
	ps1Data := exeData[payloadStart : payloadStart+int(m.PS1Size)]
	zipData := exeData[payloadStart+int(m.PS1Size) : manifestStart]

	// Create temp directory
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%s-install", m.Name, m.Version))
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp dir: %v\n", err)
		os.Exit(1)
	}

	// Write install.ps1
	ps1Path := filepath.Join(tmpDir, "install.ps1")
	if err := os.WriteFile(ps1Path, ps1Data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing install.ps1: %v\n", err)
		os.Exit(1)
	}

	// Write payload.zip
	zipPath := filepath.Join(tmpDir, "payload.zip")
	if err := os.WriteFile(zipPath, zipData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing payload.zip: %v\n", err)
		os.Exit(1)
	}

	// Run PowerShell installer
	defaultDir := filepath.Join("C:", "Program Files", m.Name)
	cmd := exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-File", ps1Path,
		"-PayloadDir", tmpDir,
		"-InstallDir", defaultDir,
		"-Action", "install")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Install failed: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Initialize stub module dependencies**

Run:
```bash
cd cmd/onepkg/installer/stub-src && go mod tidy
```
Expected: success (no output, go.mod may be unchanged)

- [ ] **Step 4: Build AMD64 stub**

Run:
```bash
cd cmd/onepkg/installer/stub-src && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-H windowsgui" -o ../stubs/wrapper-stub-amd64.exe .
```
Expected: success, creates `cmd/onepkg/installer/stubs/wrapper-stub-amd64.exe`

- [ ] **Step 5: Build ARM64 stub**

Run:
```bash
cd cmd/onepkg/installer/stub-src && GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-H windowsgui" -o ../stubs/wrapper-stub-arm64.exe .
```
Expected: success, creates `cmd/onepkg/installer/stubs/wrapper-stub-arm64.exe`

- [ ] **Step 6: Verify stubs exist**

Run:
```bash
ls -la cmd/onepkg/installer/stubs/
```
Expected: both `.exe` files present, non-zero size

- [ ] **Step 7: Commit**

```bash
git add cmd/onepkg/installer/stub-src/ cmd/onepkg/installer/stubs/
git commit -m "feat(installer): add pre-compiled Windows stub executables"
```

---

### Task 6: EXE Wrapper Generator

**Files:**
- Create: `cmd/onepkg/installer/exewrapper.go`
- Create: `cmd/onepkg/installer/exewrapper_test.go`

**Interfaces:**
- Consumes: embedded stubs (`stubs/wrapper-stub-amd64.exe`, `stubs/wrapper-stub-arm64.exe`)
- Produces: `ExeWrapperConfig` struct; `GenerateExeWrapper(cfg ExeWrapperConfig) ([]byte, error)`; `appendPayload(stub []byte, cfg ExeWrapperConfig) ([]byte, error)`.

- [ ] **Step 1: Write the failing tests**

Create `cmd/onepkg/installer/exewrapper_test.go`:

```go
package installer

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

func TestAppendPayload(t *testing.T) {
	fakeStub := []byte("FAKE_STUB_CONTENT")
	cfg := ExeWrapperConfig{
		Name:          "myapp",
		Version:       "v1.0.0",
		Platform:      "windows-amd64",
		InstallScript: "Write-Host 'install'",
		PayloadZip:    []byte("ZIP_DATA_BYTES"),
	}

	result, err := appendPayload(fakeStub, cfg)
	if err != nil {
		t.Fatalf("appendPayload returned error: %v", err)
	}

	// Result must start with the stub
	if !bytes.HasPrefix(result, fakeStub) {
		t.Error("result does not start with stub bytes")
	}

	// Result must end with magic marker
	if !bytes.HasSuffix(result, []byte(magicMarker)) {
		t.Error("result does not end with magic marker")
	}

	// Result must be: stub + ps1 + zip + manifest + manifestSize(4) + magic
	expectedLen := len(fakeStub) + len(cfg.InstallScript) + len(cfg.PayloadZip)
	// manifestSize(4) + magic(6) = 10 minimum trailer
	if len(result) <= expectedLen+10 {
		t.Errorf("result length = %d, expected > %d", len(result), expectedLen+10)
	}
}

func TestAppendPayloadManifest(t *testing.T) {
	fakeStub := []byte("STUB")
	cfg := ExeWrapperConfig{
		Name:          "testapp",
		Version:       "v2.0.0",
		Platform:      "windows-arm64",
		InstallScript: "ps1-content",
		PayloadZip:    []byte("zip-content"),
	}

	result, err := appendPayload(fakeStub, cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Parse back: magic marker at end
	magicLen := len(magicMarker)
	manifestSize := binary.LittleEndian.Uint32(result[len(result)-magicLen-4:])
	manifestStart := len(result) - magicLen - 4 - int(manifestSize)

	var m exeManifest
	if err := json.Unmarshal(result[manifestStart:manifestStart+int(manifestSize)], &m); err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	if m.Name != "testapp" {
		t.Errorf("manifest.Name = %q, want %q", m.Name, "testapp")
	}
	if m.Version != "v2.0.0" {
		t.Errorf("manifest.Version = %q, want %q", m.Version, "v2.0.0")
	}
	if m.PS1Size != int64(len("ps1-content")) {
		t.Errorf("manifest.PS1Size = %d, want %d", m.PS1Size, len("ps1-content"))
	}
	if m.ZipSize != int64(len("zip-content")) {
		t.Errorf("manifest.ZipSize = %d, want %d", m.ZipSize, len("zip-content"))
	}
}

func TestGenerateExeWrapper(t *testing.T) {
	cfg := ExeWrapperConfig{
		Name:          "myapp",
		Version:       "v1.0.0",
		Platform:      "windows-amd64",
		InstallScript: "Write-Host 'hello'",
		PayloadZip:    []byte("zipdata"),
	}

	result, err := GenerateExeWrapper(cfg)
	if err != nil {
		t.Fatalf("GenerateExeWrapper returned error: %v", err)
	}

	// Must end with magic marker
	if !bytes.HasSuffix(result, []byte(magicMarker)) {
		t.Error("exe does not end with magic marker")
	}

	// Must be larger than the stub alone (stubs are ~2MB+)
	if len(result) < 1000000 {
		t.Errorf("exe size = %d, expected > 1MB (stub + payload)", len(result))
	}

	// Must start with MZ (Windows PE header)
	if result[0] != 'M' || result[1] != 'Z' {
		t.Error("exe does not start with MZ (Windows PE header)")
	}
}

func TestGenerateExeWrapperArm64(t *testing.T) {
	cfg := ExeWrapperConfig{
		Name:          "myapp",
		Version:       "v1.0.0",
		Platform:      "windows-arm64",
		InstallScript: "Write-Host 'hello'",
		PayloadZip:    []byte("zipdata"),
	}

	result, err := GenerateExeWrapper(cfg)
	if err != nil {
		t.Fatalf("GenerateExeWrapper returned error: %v", err)
	}

	if !bytes.HasSuffix(result, []byte(magicMarker)) {
		t.Error("arm64 exe does not end with magic marker")
	}
}

func TestGenerateExeWrapperUnsupportedPlatform(t *testing.T) {
	cfg := ExeWrapperConfig{
		Name:          "myapp",
		Version:       "v1.0.0",
		Platform:      "linux-amd64", // not windows
		InstallScript: "script",
		PayloadZip:    []byte("data"),
	}

	_, err := GenerateExeWrapper(cfg)
	if err == nil {
		t.Error("expected error for non-windows platform")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error = %v, want error containing 'unsupported'", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test ./installer/ -run "TestAppendPayload|TestGenerateExeWrapper" -v`
Expected: FAIL — `undefined: ExeWrapperConfig`, `undefined: appendPayload`, `undefined: magicMarker`

- [ ] **Step 3: Write the implementation**

Create `cmd/onepkg/installer/exewrapper.go`:

```go
package installer

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed stubs/wrapper-stub-amd64.exe
var stubAmd64 []byte

//go:embed stubs/wrapper-stub-arm64.exe
var stubArm64 []byte

const magicMarker = "ONEPKG"

// ExeWrapperConfig configures the generated self-extracting exe
type ExeWrapperConfig struct {
	Name          string
	Version       string
	Platform      string // e.g., windows-amd64
	InstallScript string // install.ps1 content
	PayloadZip    []byte // payload.zip bytes
}

type exeManifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	PS1Size int64  `json:"ps1_size"`
	ZipSize int64  `json:"zip_size"`
}

// GenerateExeWrapper produces a self-extracting Windows exe by appending
// payload data to a pre-compiled stub. The stub reads its own file at runtime
// to find the appended payload.
func GenerateExeWrapper(cfg ExeWrapperConfig) ([]byte, error) {
	var stub []byte
	if strings.Contains(cfg.Platform, "amd64") {
		stub = stubAmd64
	} else if strings.Contains(cfg.Platform, "arm64") {
		stub = stubArm64
	} else {
		return nil, fmt.Errorf("unsupported windows platform: %s (expected amd64 or arm64)", cfg.Platform)
	}
	return appendPayload(stub, cfg)
}

// appendPayload appends install.ps1 + payload.zip + manifest + size + magic
// to the given stub bytes. This is the testable core of GenerateExeWrapper.
func appendPayload(stub []byte, cfg ExeWrapperConfig) ([]byte, error) {
	manifest := exeManifest{
		Name:    cfg.Name,
		Version: cfg.Version,
		PS1Size: int64(len(cfg.InstallScript)),
		ZipSize: int64(len(cfg.PayloadZip)),
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	var buf bytes.Buffer
	buf.Write(stub)
	buf.WriteString(cfg.InstallScript)
	buf.Write(cfg.PayloadZip)
	buf.Write(manifestData)

	manifestSizeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(manifestSizeBytes, uint32(len(manifestData)))
	buf.Write(manifestSizeBytes)

	buf.WriteString(magicMarker)

	return buf.Bytes(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test ./installer/ -run "TestAppendPayload|TestGenerateExeWrapper" -v`
Expected: PASS

- [ ] **Step 5: Run full installer test suite**

Run: `cd cmd/onepkg && go test ./installer/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/onepkg/installer/exewrapper.go cmd/onepkg/installer/exewrapper_test.go
git commit -m "feat(installer): add EXE wrapper generator with embedded stubs"
```

---

### Task 7: Main Orchestration — BuildForWindows

**Files:**
- Modify: `cmd/onepkg/main.go`
- Modify: `cmd/onepkg/main_test.go`

**Interfaces:**
- Consumes: `GeneratePowerShellInstaller`, `GenerateExeWrapper`, `BuildZipWithExtras`, `VirtualFile` (from Task 3, 4, 6)
- Produces: `BuildForWindows` function; updated `BuildForPlatform` switch with `windows` case; updated `platformOutputPath` returning `.exe` for Windows; updated main loop to skip tar.gz build for Windows.

- [ ] **Step 1: Write the failing integration test**

Append to `cmd/onepkg/main_test.go`:

```go
func TestBuildForWindows(t *testing.T) {
	projectDir := t.TempDir()

	binDir := filepath.Join(projectDir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "myapp.exe"), []byte("fake-exe-content"), 0755)

	configContent := `
name = "myapp"
version = "v1.0.0"

items = ["bin/myapp.exe"]

[[services]]
name = "myapp"
exec = "bin/myapp.exe"

[services.windows]
start_type = "auto"
recovery_delay_ms = 5000
`
	configPath := filepath.Join(projectDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ApplyDefaults()

	platform := "windows-amd64"
	resolved, err := ResolveItems(projectDir, cfg.Items, platform)
	if err != nil {
		t.Fatalf("ResolveItems: %v", err)
	}

	entries, err := CollectFilesResolved(projectDir, resolved, cfg.Exclude)
	if err != nil {
		t.Fatalf("CollectFilesResolved: %v", err)
	}

	shaMap := make(map[string]string)
	for _, e := range entries {
		shaMap[e.RelPath] = e.SHA1
	}

	outputDir := filepath.Join(projectDir, "packages")
	os.MkdirAll(outputDir, 0755)

	// For Windows, tarData is nil (built internally)
	if err := BuildForPlatform(cfg, platform, entries, nil, shaMap, outputDir); err != nil {
		t.Fatalf("BuildForPlatform: %v", err)
	}

	// Verify output file exists with .exe extension
	expectedFile := filepath.Join(outputDir, "myapp_v1.0.0_windows_amd64.exe")
	if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
		t.Fatalf("expected output file %s does not exist", expectedFile)
	}

	// Verify exe ends with magic marker
	data, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("ONEPKG")) {
		t.Error("exe does not end with ONEPKG magic marker")
	}
	// Verify exe starts with MZ (PE header)
	if data[0] != 'M' || data[1] != 'Z' {
		t.Error("exe does not start with MZ (Windows PE header)")
	}
}

func TestPlatformOutputPathWindows(t *testing.T) {
	cfg := &Config{Name: "myapp", Version: "v1.0.0"}

	tests := []struct {
		platform string
		want     string
	}{
		{"linux-amd64", "myapp_v1.0.0_linux_amd64.sh"},
		{"darwin-arm64", "myapp_v1.0.0_darwin_arm64.sh"},
		{"windows-amd64", "myapp_v1.0.0_windows_amd64.exe"},
		{"windows-arm64", "myapp_v1.0.0_windows_arm64.exe"},
	}

	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			got := filepath.Base(platformOutputPath(cfg, tt.platform, ""))
			if got != tt.want {
				t.Errorf("platformOutputPath(%q) = %q, want %q", tt.platform, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run "TestBuildForWindows|TestPlatformOutputPathWindows" -v`
Expected: FAIL — Windows case not handled, `.exe` extension not returned

- [ ] **Step 3: Update platformOutputPath for .exe extension**

In `cmd/onepkg/main.go`, find:

```go
func platformOutputPath(cfg *Config, platform string, outputDir string) string {
	platformFile := strings.ReplaceAll(platform, "-", "_")
	return filepath.Join(outputDir, fmt.Sprintf("%s_%s_%s.sh", cfg.Name, cfg.Version, platformFile))
}
```

Replace with:

```go
func platformOutputPath(cfg *Config, platform string, outputDir string) string {
	platformFile := strings.ReplaceAll(platform, "-", "_")
	goos := strings.SplitN(platform, "-", 2)[0]
	ext := ".sh"
	if goos == "windows" {
		ext = ".exe"
	}
	return filepath.Join(outputDir, fmt.Sprintf("%s_%s_%s%s", cfg.Name, cfg.Version, platformFile, ext))
}
```

- [ ] **Step 4: Add windows case to BuildForPlatform**

In `cmd/onepkg/main.go`, find the `BuildForPlatform` function. It currently has a switch on `goos` with `linux`, `darwin`, and `default` cases. Find the `default:` case:

```go
	default:
		defaultDir = "/usr/local/" + cfg.Name
	}
```

Add a `windows` case before `default`:

```go
	case "windows":
		return BuildForWindows(cfg, platform, entries, shaMap, outputDir)
	default:
		defaultDir = "/usr/local/" + cfg.Name
	}
```

- [ ] **Step 5: Add BuildForWindows function**

In `cmd/onepkg/main.go`, add this function after the `BuildForPlatform` function:

```go
func BuildForWindows(cfg *Config, platform string, entries []FileEntry, shaMap map[string]string, outputDir string) error {
	parts := strings.SplitN(platform, "-", 2)
	arch := parts[1]

	defaultDir := filepath.Join("C:", "Program Files", cfg.Name)

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

	// Build ZIP payload with VERSION file included
	payloadZip, err := BuildZipWithExtras(entries, *cfg.PreservePermissions, []VirtualFile{
		{RelPath: "VERSION", Content: []byte(versionContent), Mode: 0644},
	})
	if err != nil {
		return fmt.Errorf("build zip: %w", err)
	}

	// Build Windows service config (if service defined)
	var serviceName string
	var serviceConfig installer.WindowsServiceConfig
	if len(cfg.Services) > 0 {
		svc := cfg.Services[0]
		serviceName = svc.Name
		serviceConfig = installer.WindowsServiceConfig{
			StartType:       svc.Windows.StartType,
			RecoveryDelayMs: svc.Windows.RecoveryDelayMs,
			Exec:            filepath.Join(defaultDir, svc.Exec),
			Args:            svc.Args,
		}
	}

	// Convert DirConfig to DirSpec
	var dirs []installer.DirSpec
	for _, d := range cfg.Dirs {
		dirs = append(dirs, installer.DirSpec{
			Path:  d.Path,
			Owner: d.Owner,
			Group: d.Group,
			Mode:  d.Mode,
		})
	}

	// Convert OverlayConfig to OverlaySpec
	var overlays []installer.OverlaySpec
	for _, o := range cfg.SystemOverlay {
		overlays = append(overlays, installer.OverlaySpec{
			System: o.System,
			Item:   o.Item,
		})
	}

	// Generate install.ps1
	ps1Content, err := installer.GeneratePowerShellInstaller(installer.PowerShellConfig{
		Name:          cfg.Name,
		Version:       cfg.Version,
		Platform:      platform,
		ExpectedArch:  unameArch("windows", arch),
		DefaultDir:    defaultDir,
		ServiceName:   serviceName,
		ServiceConfig: serviceConfig,
		Dirs:          dirs,
		Overlays:      overlays,
	})
	if err != nil {
		return fmt.Errorf("generate powershell: %w", err)
	}

	// Generate self-extracting exe
	exeData, err := installer.GenerateExeWrapper(installer.ExeWrapperConfig{
		Name:          cfg.Name,
		Version:       cfg.Version,
		Platform:      platform,
		InstallScript: ps1Content,
		PayloadZip:    payloadZip,
	})
	if err != nil {
		return fmt.Errorf("generate exe wrapper: %w", err)
	}

	outputPath := platformOutputPath(cfg, platform, outputDir)
	return os.WriteFile(outputPath, exeData, 0755)
}
```

- [ ] **Step 6: Update main loop to skip tar.gz for Windows**

In `cmd/onepkg/main.go`, find the main loop that builds tar.gz:

```go
		tarData, err := BuildTarGZ(entries, *cfg.PreservePermissions)
		if err != nil {
			fmt.Printf("Error building archive for %s: %v\n", platform, err)
			os.Exit(1)
		}
```

Replace with a platform-aware version:

```go
		goos := strings.SplitN(platform, "-", 2)[0]
		var tarData []byte
		if goos != "windows" {
			tarData, err = BuildTarGZ(entries, *cfg.PreservePermissions)
			if err != nil {
				fmt.Printf("Error building archive for %s: %v\n", platform, err)
				os.Exit(1)
			}
		}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test -run "TestBuildForWindows|TestPlatformOutputPathWindows" -v`
Expected: PASS

- [ ] **Step 8: Run full test suite**

Run: `cd cmd/onepkg && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 9: Commit**

```bash
git add cmd/onepkg/main.go cmd/onepkg/main_test.go
git commit -m "feat(main): add BuildForWindows orchestration and .exe output"
```

---

### Task 8: Update Documentation and Config Example

**Files:**
- Modify: `docs/config-example.toml`
- Modify: `CLAUDE.md`
- Modify: `README.md`

**Interfaces:**
- None (documentation only)

- [ ] **Step 1: Add Windows example to config-example.toml**

In `docs/config-example.toml`, after the macOS service section (after the `[services.macos]` block, before the `# Optional: system overlays` comment), add:

```toml

# Windows-specific (Windows Service) settings
[services.windows]
start_type = "auto"            # start type: auto (default) | manual | disabled
                              # - auto: service starts at boot
                              # - manual: service starts manually or on demand
                              # - disabled: service cannot start
recovery_delay_ms = 5000       # delay before restart on failure in ms (default: 5000)
                              # Sets service recovery to restart after this delay
```

- [ ] **Step 2: Update CLAUDE.md MVP scope**

In `CLAUDE.md`, find:

```
- MVP scope: Linux/macOS only. Windows `.ps1` and Windows Service are post-MVP.
```

Replace with:

```
- Supported platforms: Linux (systemd), macOS (launchd), Windows (Windows Service via sc.exe).
- Windows installers are self-extracting `.exe` files using pre-compiled stubs (see `installer/stubs/`).
- To rebuild stubs after changing `installer/stub-src/wrapper.go`:
  `cd cmd/onepkg/installer/stub-src && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-H windowsgui" -o ../stubs/wrapper-stub-amd64.exe .`
  (repeat with GOARCH=arm64 for the ARM64 stub)
```

- [ ] **Step 3: Update README.md**

In `README.md`, find:

```
**MVP:** Linux and macOS only. Windows support (`.ps1`, Windows Service) is post-MVP.
```

Replace with:

```
**Platforms:** Linux (systemd), macOS (launchd), and Windows (Windows Service). Windows installers are self-extracting `.exe` files.
```

- [ ] **Step 4: Commit**

```bash
git add docs/config-example.toml CLAUDE.md README.md
git commit -m "docs: document Windows platform support"
```

---

## Self-Review

### 1. Spec Coverage

| Spec Requirement | Task |
|---|---|
| windows-amd64 and windows-arm64 architectures | Task 2 (platform), Task 5 (stubs) |
| Windows 8/10/11 target (ZIP, not tar.gz) | Task 3 (ZIP building) |
| Double-click .exe output | Task 5 (stub), Task 6 (wrapper), Task 7 (orchestration) |
| Default path C:\Program Files\{name} | Task 7 (BuildForWindows) |
| start_type config field (auto/manual/disabled) | Task 1 (config) |
| recovery_delay_ms config field | Task 1 (config) |
| Defaults: start_type=auto, recovery_delay_ms=5000 | Task 1 (config) |
| Validation: start_type values, recovery_delay_ms >= 0 | Task 1 (config) |
| ZIP payload format | Task 3 (packager) |
| sc.exe service management | Task 4 (powershell) |
| Service creation with recovery | Task 4 (powershell) |
| Service removal | Task 4 (powershell) |
| Admin privilege check (PowerShell security principal) | Task 4 (powershell) |
| Architecture check ($env:PROCESSOR_ARCHITECTURE) | Task 4 (powershell) |
| Pre-compiled stub embedded in onepkg | Task 5 (stub build), Task 6 (embed) |
| Stub reads own exe, finds magic marker | Task 5 (stub source) |
| Manifest JSON with name/version/ps1_size/zip_size | Task 6 (exewrapper) |
| install/uninstall/version/list/verify commands | Task 4 (powershell) |
| Output naming: {name}_{version}_windows_{arch}.exe | Task 7 (platformOutputPath) |
| dirs support | Task 4 (powershell CreateDirs) |
| system_overlay support | Task 4 (powershell ApplyOverlays) |

### 2. Placeholder Scan

No TBD, TODO, or placeholder patterns. All steps contain complete code.

### 3. Type Consistency

- `WindowsService` (config.go, Task 1) — fields `StartType string`, `RecoveryDelayMs int`
- `ServiceConfig.Windows` (Task 1) — used in Task 7 BuildForWindows
- `VirtualFile` (packager.go, Task 3) — fields `RelPath`, `Content []byte`, `Mode fs.FileMode`; used in Task 7
- `BuildZipWithExtras` signature (Task 3): `(entries []FileEntry, preservePermissions bool, extras []VirtualFile) ([]byte, error)` — called in Task 7 with matching args
- `WindowsServiceConfig` (powershell.go, Task 4) — fields `StartType`, `RecoveryDelayMs`, `Exec`, `Args`; constructed in Task 7
- `PowerShellConfig` (Task 4) — fields match Task 7 construction
- `ExeWrapperConfig` (exewrapper.go, Task 6) — fields `Name`, `Version`, `Platform`, `InstallScript`, `PayloadZip`; constructed in Task 7
- `exeManifest` (Task 6) — fields `Name`, `Version`, `PS1Size`, `ZipSize`; matches stub's `manifest` struct (Task 5)
- `magicMarker = "ONEPKG"` — defined in both exewrapper.go (Task 6) and stub wrapper.go (Task 5), identical value
- `DirSpec`, `OverlaySpec` — existing types reused in PowerShellConfig (Task 4)
- `unameArch` (Task 2) — Windows cases added before generic amd64 case
- `platformOutputPath` (Task 7) — returns `.exe` for windows goos
