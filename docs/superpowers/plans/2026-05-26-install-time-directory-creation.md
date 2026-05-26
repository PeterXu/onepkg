# Install-Time Directory Creation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `dirs` config field allowing packages to create directories at install time with configurable ownership and permissions.

**Architecture:** Add DirConfig struct to config.go, generate shell variables (DIRS_COUNT, DIR_0_PATH, etc.) in shell.go, create_dirs() function runs at install time.

**Tech Stack:** Go 1.21, TOML config, bash installer

---

## File Structure

| File | Purpose |
|------|---------|
| `cmd/onepkg/config.go` | DirConfig struct, Dirs field, validation |
| `cmd/onepkg/config_test.go` | Tests for parsing and validation |
| `cmd/onepkg/installer/shell.go` | ShellConfig.Dirs, DIR_* variable generation, create_dirs() |
| `cmd/onepkg/installer/shell_test.go` | Tests for generated script |
| `cmd/onepkg/main.go` | Pass cfg.Dirs to ShellConfig |

---

### Task 1: Add DirConfig Struct and Parsing

**Files:**
- Modify: `cmd/onepkg/config.go`
- Test: `cmd/onepkg/config_test.go`

- [ ] **Step 1: Write failing test for parsing dirs**

```go
// In config_test.go, add after TestParseConfigWithLimitNOFILE:

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestParseConfigWithDirs -v`
Expected: FAIL (field Dirs does not exist)

- [ ] **Step 3: Add DirConfig struct to config.go**

```go
// In config.go, add after MacOSService struct (around line 48):

type DirConfig struct {
    Path  string `toml:"path"`   // Required
    Owner string `toml:"owner"`  // Optional, default: service user or root
    Group string `toml:"group"`  // Optional, default: same as owner
    Mode  string `toml:"mode"`   // Optional, default: "0755"
}
```

```go
// In config.go, add to Config struct (around line 21):

type Config struct {
    Name               string          `toml:"name"`
    Version            string          `toml:"version"`
    Platforms          []string        `toml:"platforms"`
    OutputDir          string          `toml:"output_dir"`
    PreservePermissions *bool          `toml:"preserve_permissions"`
    Exclude            []string        `toml:"exclude"`
    Items              []string        `toml:"items"`
    Dirs               []DirConfig     `toml:"dirs"`    // NEW
    Services           []ServiceConfig `toml:"services"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run TestParseConfigWithDirs -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/config.go cmd/onepkg/config_test.go
git commit -m "feat(config): add dirs field for install-time directory creation"
```

---

### Task 2: Add Dir Validation

**Files:**
- Modify: `cmd/onepkg/config.go`
- Test: `cmd/onepkg/config_test.go`

- [ ] **Step 1: Write failing tests for dir validation**

```go
// In config_test.go, add:

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestValidateDirs -v`
Expected: FAIL (validation not implemented)

- [ ] **Step 3: Add validation helper functions**

```go
// In config.go, add after isValidName function (around line 157):

// criticalPaths is a blacklist of system paths that must never be created by installers
var criticalPaths = map[string]bool{
    "/etc/passwd":     true,
    "/etc/shadow":     true,
    "/etc/sudoers":    true,
    "/root":           true,
    "/boot":           true,
    "/sys":            true,
    "/proc":           true,
    "/dev":            true,
}

// isCriticalPath checks if a path is a critical system path or under one
func isCriticalPath(path string) bool {
    // Check exact match
    if criticalPaths[path] {
        return true
    }
    // Check if path is under a critical directory
    for critical := range criticalPaths {
        if strings.HasPrefix(path, critical+"/") {
            return true
        }
    }
    return false
}

// isValidMode checks if mode is a valid octal permission string (0[0-7]{3,4})
func isValidMode(mode string) bool {
    if len(mode) < 4 || len(mode) > 5 {
        return false
    }
    if mode[0] != '0' {
        return false
    }
    for _, c := range mode[1:] {
        if c < '0' || c > '7' {
            return false
        }
    }
    return true
}
```

- [ ] **Step 4: Add dirs validation to Validate function**

```go
// In config.go, in Validate function, add after service validation (around line 146):

    // Dirs validation
    for _, d := range c.Dirs {
        if d.Path == "" {
            return fmt.Errorf("dirs path is required")
        }
        // Check path traversal
        cleanPath := filepath.Clean(d.Path)
        if strings.Contains(cleanPath, "..") {
            return fmt.Errorf("path traversal not allowed in dirs path %q", d.Path)
        }
        // Check critical system paths (only for absolute paths)
        if filepath.IsAbs(d.Path) && isCriticalPath(cleanPath) {
            return fmt.Errorf("critical system path not allowed: %q", d.Path)
        }
        // Validate mode if specified
        if d.Mode != "" && !isValidMode(d.Mode) {
            return fmt.Errorf("invalid mode %q for dirs path %q (must be octal like 0755)", d.Mode, d.Path)
        }
        // Validate owner/group if specified
        if d.Owner != "" && !isValidName(d.Owner) {
            return fmt.Errorf("invalid owner %q for dirs path %q", d.Owner, d.Path)
        }
        if d.Group != "" && !isValidName(d.Group) {
            return fmt.Errorf("invalid group %q for dirs path %q", d.Group, d.Path)
        }
    }
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run TestValidateDirs -v`
Expected: PASS

- [ ] **Step 6: Run all config tests**

Run: `cd cmd/onepkg && go test -run TestParseConfig -v`
Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/onepkg/config.go cmd/onepkg/config_test.go
git commit -m "feat(config): add dirs validation for path safety and mode format"
```

---

### Task 3: Add ApplyDefaults for Dirs

**Files:**
- Modify: `cmd/onepkg/config.go`
- Test: `cmd/onepkg/config_test.go`

- [ ] **Step 1: Write failing test for dirs defaults**

```go
// In config_test.go, add:

func TestApplyDefaultsDirs(t *testing.T) {
    tests := []struct {
        name     string
        cfg      *Config
        wantPath string
        wantOwner string
        wantGroup string
        wantMode string
    }{
        {
            name: "defaults from service user",
            cfg: &Config{
                Name: "myapp", Version: "v1.0.0",
                Items: []string{"bin/myapp"},
                Dirs:  []DirConfig{{Path: "/var/log/myapp"}},
                Services: []ServiceConfig{{Name: "myapp", User: "appuser"}},
            },
            wantPath: "/var/log/myapp",
            wantOwner: "appuser",
            wantGroup: "appuser",
            wantMode: "0755",
        },
        {
            name: "defaults without service (root)",
            cfg: &Config{
                Name: "myapp", Version: "v1.0.0",
                Items: []string{"bin/myapp"},
                Dirs:  []DirConfig{{Path: "/var/log/myapp"}},
            },
            wantPath: "/var/log/myapp",
            wantOwner: "root",
            wantGroup: "root",
            wantMode: "0755",
        },
        {
            name: "explicit owner preserved",
            cfg: &Config{
                Name: "myapp", Version: "v1.0.0",
                Items: []string{"bin/myapp"},
                Dirs:  []DirConfig{{Path: "/var/log/myapp", Owner: "loguser"}},
                Services: []ServiceConfig{{Name: "myapp", User: "appuser"}},
            },
            wantPath: "/var/log/myapp",
            wantOwner: "loguser",
            wantGroup: "loguser",
            wantMode: "0755",
        },
        {
            name: "explicit group preserved",
            cfg: &Config{
                Name: "myapp", Version: "v1.0.0",
                Items: []string{"bin/myapp"},
                Dirs:  []DirConfig{{Path: "/var/log/myapp", Owner: "loguser", Group: "loggroup"}},
            },
            wantPath: "/var/log/myapp",
            wantOwner: "loguser",
            wantGroup: "loggroup",
            wantMode: "0755",
        },
        {
            name: "explicit mode preserved",
            cfg: &Config{
                Name: "myapp", Version: "v1.0.0",
                Items: []string{"bin/myapp"},
                Dirs:  []DirConfig{{Path: "/var/log/myapp", Mode: "0750"}},
            },
            wantPath: "/var/log/myapp",
            wantOwner: "root",
            wantGroup: "root",
            wantMode: "0750",
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestApplyDefaultsDirs -v`
Expected: FAIL (defaults not applied)

- [ ] **Step 3: Add dirs defaults to ApplyDefaults**

```go
// In config.go, in ApplyDefaults function, add at end (around line 86):

    // Dirs defaults
    defaultUser := "root"
    if len(c.Services) > 0 && c.Services[0].User != "" {
        defaultUser = c.Services[0].User
    }
    for i := range c.Dirs {
        if c.Dirs[i].Owner == "" {
            c.Dirs[i].Owner = defaultUser
        }
        if c.Dirs[i].Group == "" {
            c.Dirs[i].Group = c.Dirs[i].Owner
        }
        if c.Dirs[i].Mode == "" {
            c.Dirs[i].Mode = "0755"
        }
    }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run TestApplyDefaultsDirs -v`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `cd cmd/onepkg && go test -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/onepkg/config.go cmd/onepkg/config_test.go
git commit -m "feat(config): apply defaults to dirs (owner, group, mode)"
```

---

### Task 4: Add Dirs to ShellConfig and Generator

**Files:**
- Modify: `cmd/onepkg/installer/shell.go`
- Test: `cmd/onepkg/installer/shell_test.go`

- [ ] **Step 1: Write failing test for dirs in shell output**

```go
// In shell_test.go, add:

func TestGenerateShellInstallerWithDirs(t *testing.T) {
    cfg := ShellConfig{
        Name:         "myapp",
        Version:      "v1.0.0",
        Platform:     "linux-arm64",
        ExpectedArch: "aarch64",
        DefaultDir:   "/usr/local/myapp",
        PayloadBase64: "dGVzdA==",
        VersionContent: "dGVzdA==",
        ServiceType:  "none",
        ServiceName:  "",
        Dirs: []DirSpec{
            {Path: "/var/log/myapp", Owner: "myapp", Group: "myapp", Mode: "0755"},
            {Path: "data", Owner: "myapp", Group: "myapp", Mode: "0750"},
        },
    }

    output, err := GenerateShellInstaller(cfg)
    if err != nil {
        t.Fatalf("GenerateShellInstaller returned error: %v", err)
    }

    // Check DIRS_COUNT
    if !strings.Contains(output, "DIRS_COUNT=2") {
        t.Error("output missing DIRS_COUNT=2")
    }

    // Check DIR_0 variables
    if !strings.Contains(output, "DIR_0_PATH=\"/var/log/myapp\"") {
        t.Error("output missing DIR_0_PATH")
    }
    if !strings.Contains(output, "DIR_0_OWNER=\"myapp\"") {
        t.Error("output missing DIR_0_OWNER")
    }
    if !strings.Contains(output, "DIR_0_GROUP=\"myapp\"") {
        t.Error("output missing DIR_0_GROUP")
    }
    if !strings.Contains(output, "DIR_0_MODE=\"0755\"") {
        t.Error("output missing DIR_0_MODE")
    }

    // Check DIR_1 variables (relative path)
    if !strings.Contains(output, "DIR_1_PATH=\"data\"") {
        t.Error("output missing DIR_1_PATH")
    }

    // Check create_dirs function exists
    if !strings.Contains(output, "create_dirs()") {
        t.Error("output missing create_dirs function")
    }
}

func TestGenerateShellInstallerNoDirs(t *testing.T) {
    cfg := ShellConfig{
        Name:         "myapp",
        Version:      "v1.0.0",
        Platform:     "linux-arm64",
        ExpectedArch: "aarch64",
        DefaultDir:   "/usr/local/myapp",
        PayloadBase64: "dGVzdA==",
        VersionContent: "dGVzdA==",
        ServiceType:  "none",
        ServiceName:  "",
        Dirs:         []DirSpec{}, // Empty
    }

    output, err := GenerateShellInstaller(cfg)
    if err != nil {
        t.Fatalf("GenerateShellInstaller returned error: %v", err)
    }

    // Should have DIRS_COUNT=0
    if !strings.Contains(output, "DIRS_COUNT=0") {
        t.Error("output should have DIRS_COUNT=0 for empty dirs")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test ./installer/ -run TestGenerateShellInstallerWithDirs -v`
Expected: FAIL (DirSpec and Dirs field don't exist)

- [ ] **Step 3: Add DirSpec and update ShellConfig**

```go
// In shell.go, add after ShellConfig struct (around line 19):

type DirSpec struct {
    Path  string
    Owner string
    Group string
    Mode  string
}
```

```go
// In shell.go, update ShellConfig struct to include Dirs:

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
    Dirs           []DirSpec  // NEW
}
```

- [ ] **Step 4: Add DIR_* variable generation**

```go
// In shell.go, in GenerateShellInstaller function, after ServiceName (around line 35):

    fmt.Fprintf(&b, "DIRS_COUNT=%d\n", len(cfg.Dirs))
    for i, d := range cfg.Dirs {
        fmt.Fprintf(&b, "DIR_%d_PATH=\"%s\"\n", i, d.Path)
        fmt.Fprintf(&b, "DIR_%d_OWNER=\"%s\"\n", i, d.Owner)
        fmt.Fprintf(&b, "DIR_%d_GROUP=\"%s\"\n", i, d.Group)
        fmt.Fprintf(&b, "DIR_%d_MODE=\"%s\"\n", i, d.Mode)
    }
    b.WriteString("\n")
```

- [ ] **Step 5: Add create_dirs function**

```go
// In shell.go, add create_dirs function in the helper functions section (after uninstall_service, around line 98):

create_dirs() {
    local install_dir="$1"

    if [ "$DIRS_COUNT" -eq 0 ] || [ -z "$DIRS_COUNT" ]; then
        return
    fi

    for i in $(seq 0 $((DIRS_COUNT - 1))); do
        eval "dir_path=\"\$DIR_${i}_PATH\""
        eval "dir_owner=\"\$DIR_${i}_OWNER\""
        eval "dir_group=\"\$DIR_${i}_GROUP\""
        eval "dir_mode=\"\$DIR_${i}_MODE\""

        # Resolve relative paths
        if [ "${dir_path:0:1}" != "/" ]; then
            dir_path="$install_dir/$dir_path"
        fi

        mkdir -p "$dir_path"

        if [ -n "$dir_owner" ]; then
            chown "${dir_owner}:${dir_group:-$dir_owner}" "$dir_path"
        fi

        if [ -n "$dir_mode" ]; then
            chmod "$dir_mode" "$dir_path"
        fi
    done
}

`)
```

- [ ] **Step 6: Call create_dirs in cmd_install**

```go
// In shell.go, in cmd_install function, after VERSION file creation (around line 128-130):

    echo "$VERSION_B64" | base64 -d > "$install_dir/VERSION"

    # Create directories if configured
    if [ "$DIRS_COUNT" -gt 0 ]; then
        echo "Creating directories..."
        create_dirs "$install_dir"
    fi

    if [ "$SERVICE_TYPE" != "none" ] && [ -n "$SERVICE_B64" ]; then
```

- [ ] **Step 7: Run test to verify it passes**

Run: `cd cmd/onepkg && go test ./installer/ -run TestGenerateShellInstallerWithDirs -v`
Expected: PASS

- [ ] **Step 8: Run all installer tests**

Run: `cd cmd/onepkg && go test ./installer/ -v`
Expected: All PASS

- [ ] **Step 9: Commit**

```bash
git add cmd/onepkg/installer/shell.go cmd/onepkg/installer/shell_test.go
git commit -m "feat(shell): add dirs creation in installer with DIR_* variables"
```

---

### Task 5: Pass Dirs from Config to ShellConfig

**Files:**
- Modify: `cmd/onepkg/main.go`

- [ ] **Step 1: Build to verify it compiles**

Run: `cd cmd/onepkg && go build -o onepkg .`
Expected: Success (no Dirs field error yet)

- [ ] **Step 2: Add Dirs to ShellConfig in main.go**

```go
// In main.go, in BuildForPlatform function, update ShellConfig (around line 212-223):

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
        Dirs:           convertDirs(cfg.Dirs),  // NEW
    }
```

- [ ] **Step 3: Add convertDirs helper function**

```go
// In main.go, add after platformOutputPath function (around line 237):

func convertDirs(dirs []DirConfig) []installer.DirSpec {
    result := make([]installer.DirSpec, len(dirs))
    for i, d := range dirs {
        result[i] = installer.DirSpec{
            Path:  d.Path,
            Owner: d.Owner,
            Group: d.Group,
            Mode:  d.Mode,
        }
    }
    return result
}
```

- [ ] **Step 4: Build and verify**

Run: `cd cmd/onepkg && go build -o onepkg .`
Expected: Success

- [ ] **Step 5: Run all tests**

Run: `cd cmd/onepkg && go test ./... -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/onepkg/main.go
git commit -m "feat(main): pass dirs config to shell installer generator"
```

---

## Verification

After implementation:

1. **Run all tests:** `cd cmd/onepkg && go test ./... -v`
2. **Build:** `cd cmd/onepkg && go build -o onepkg .`
3. **Test with dirs config:**
   ```bash
   # Create config with dirs
   cat > test-config.toml << 'EOF'
   name = "testapp"
   version = "v1.0.0"
   items = ["bin/testapp"]
   dirs = [
     { path = "/var/log/testapp", owner = "root", mode = "0755" },
     { path = "data", mode = "0750" },
   ]
   EOF

   ./onepkg -c test-config.toml --platform linux-arm64
   # Inspect generated .sh, verify DIRS_COUNT and DIR_* variables
   ```
4. **Verify generated script has:**
   - `DIRS_COUNT=2`
   - `DIR_0_PATH`, `DIR_0_OWNER`, `DIR_0_MODE` variables
   - `create_dirs()` function
   - Call to `create_dirs` in `cmd_install`