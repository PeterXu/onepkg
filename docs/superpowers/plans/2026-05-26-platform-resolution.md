# Platform Resolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove static `platforms` from config.toml, make `--platform` CLI flag required with multiple-platform support, and implement `{platform}` marker resolution for items that vary per platform.

**Architecture:** Items in config.toml can use `{platform}` as a placeholder (e.g., `bin/myapp.{platform}`). At build time, onepkg resolves each marked item to the platform-specific file (e.g., `bin/myapp.linux-arm64`) and renames it to the base path in the package. Multiple platforms passed via `--platform linux-arm64,linux-amd64` produce multiple output files in one run.

**Tech Stack:** Go 1.21, `github.com/BurntSushi/toml`, standard library only.

---

## File Structure

Modifications to existing files under `cmd/onepkg/`:

```
cmd/onepkg/
├── config.go            # Remove Platforms default, add HasPlatformMarker helper
├── config_test.go       # Update tests for removed platforms default, add marker tests
├── packager.go          # Add ResolveItems function for {platform} resolution
├── packager_test.go     # Add tests for ResolveItems
├── main.go              # Make --platform required, validate format, multiple platform support
└── main_test.go         # Update integration test for required --platform
```

---

### Task 1: Remove Platforms Default from Config

**Files:**
- Modify: `cmd/onepkg/config.go:50-56`
- Modify: `cmd/onepkg/config_test.go:139-167`

- [ ] **Step 1: Write the failing test**

Modify `TestApplyDefaults` in `cmd/onepkg/config_test.go` to expect empty Platforms (no default applied):

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestApplyDefaults -v`
Expected: FAIL — `len(Platforms) = 1, want 0`

- [ ] **Step 3: Remove the Platforms default**

Modify `cmd/onepkg/config.go`, remove the Platforms default block in `ApplyDefaults`:

```go
func (c *Config) ApplyDefaults() {
	if c.OutputDir == "" {
		c.OutputDir = "packages"
	}
	// Platforms has no default - must be provided via --platform flag
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run TestApplyDefaults -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/config.go cmd/onepkg/config_test.go
git commit -m "refactor(onepkg): remove platforms default from config"
```

---

### Task 2: Add Platform Marker Helper Function

**Files:**
- Modify: `cmd/onepkg/config.go`
- Modify: `cmd/onepkg/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onepkg/config_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run "TestHasPlatformMarker|TestResolvePlatformItem|TestStripPlatformMarker" -v`
Expected: FAIL — `undefined: HasPlatformMarker`, `undefined: ResolvePlatformItem`, `undefined: StripPlatformMarker`

- [ ] **Step 3: Write the helper functions**

Append to `cmd/onepkg/config.go`:

```go
const PlatformMarker = "{platform}"

// HasPlatformMarker returns true if the item contains {platform} placeholder
func HasPlatformMarker(item string) bool {
	return strings.Contains(item, PlatformMarker)
}

// ResolvePlatformItem replaces {platform} with the actual platform string
func ResolvePlatformItem(item, platform string) string {
	return strings.ReplaceAll(item, PlatformMarker, platform)
}

// StripPlatformMarker removes {platform} from the item path
func StripPlatformMarker(item string) string {
	return strings.ReplaceAll(item, PlatformMarker, "")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run "TestHasPlatformMarker|TestResolvePlatformItem|TestStripPlatformMarker" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/config.go cmd/onepkg/config_test.go
git commit -m "feat(onepkg): add platform marker helper functions"
```

---

### Task 3: Add ResolveItems Function in Packager

**Files:**
- Modify: `cmd/onepkg/packager.go`
- Modify: `cmd/onepkg/packager_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cmd/onepkg/packager_test.go`:

```go
func TestResolveItems(t *testing.T) {
	dir := t.TempDir()

	// Create platform-specific binaries
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "myapp.linux-arm64"), []byte("binary-linux-arm64"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "myapp.linux-amd64"), []byte("binary-linux-amd64"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "helper.linux-arm64"), []byte("helper-linux"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create non-platform-specific files
	etcDir := filepath.Join(dir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "config.conf"), []byte("config-data"), 0644); err != nil {
		t.Fatal(err)
	}

	items := []string{
		"bin/myapp.{platform}",
		"bin/helper.{platform}",
		"etc/config.conf",
	}

	resolved, err := ResolveItems(dir, items, "linux-arm64")
	if err != nil {
		t.Fatalf("ResolveItems returned error: %v", err)
	}

	// Check resolved items have correct paths
	expectedResolved := map[string]string{
		"bin/myapp.{platform}":  "bin/myapp.linux-arm64",
		"bin/helper.{platform}": "bin/helper.linux-arm64",
		"etc/config.conf":       "etc/config.conf",
	}

	for original, wantResolved := range expectedResolved {
		found := false
		for _, r := range resolved {
			if r.Original == original && r.Resolved == wantResolved {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing resolved item for %q: want resolved=%q", original, wantResolved)
		}
	}

	// Check that files exist
	for _, r := range resolved {
		fullPath := filepath.Join(dir, r.Resolved)
		if _, err := os.Stat(fullPath); err != nil {
			t.Errorf("resolved file %q does not exist at %s", r.Resolved, fullPath)
		}
	}
}

func TestResolveItemsMissingPlatformFile(t *testing.T) {
	dir := t.TempDir()

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Only create linux-amd64, not linux-arm64
	if err := os.WriteFile(filepath.Join(binDir, "myapp.linux-amd64"), []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}

	items := []string{"bin/myapp.{platform}"}

	_, err := ResolveItems(dir, items, "linux-arm64")
	if err == nil {
		t.Fatal("expected error for missing platform file")
	}
	if !strings.Contains(err.Error(), "bin/myapp.linux-arm64") {
		t.Errorf("error should mention missing file, got: %v", err)
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error should say 'does not exist', got: %v", err)
	}
}

func TestResolveItemsNoFallback(t *testing.T) {
	dir := t.TempDir()

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create base file but NOT platform-specific file
	if err := os.WriteFile(filepath.Join(binDir, "myapp"), []byte("base-binary"), 0755); err != nil {
		t.Fatal(err)
	}

	items := []string{"bin/myapp.{platform}"}

	_, err := ResolveItems(dir, items, "linux-arm64")
	if err == nil {
		t.Fatal("expected error - should NOT fall back to base file")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error should mention missing platform file, got: %v", err)
	}
}
```

Add `"strings"` to imports in `packager_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestResolveItems -v`
Expected: FAIL — `undefined: ResolveItems`, `undefined: ResolvedItem`

- [ ] **Step 3: Write the ResolveItems function**

Modify `cmd/onepkg/packager.go`, add the ResolvedItem type and ResolveItems function:

```go
// ResolvedItem represents an item path after platform resolution
type ResolvedItem struct {
	Original string // Original item from config (e.g., "bin/myapp.{platform}")
	Resolved string // Resolved path (e.g., "bin/myapp.linux-arm64")
	PackPath string // Path to use in package (e.g., "bin/myapp" - marker stripped)
}

// ResolveItems resolves {platform} markers in items for a specific platform
// Returns error if a platform-specific file does not exist (no fallback)
func ResolveItems(configDir string, items []string, platform string) ([]ResolvedItem, error) {
	var resolved []ResolvedItem
	for _, item := range items {
		ri := ResolvedItem{
			Original: item,
		}

		if HasPlatformMarker(item) {
			ri.Resolved = ResolvePlatformItem(item, platform)
			ri.PackPath = StripPlatformMarker(item)

			// Verify the platform-specific file exists - NO FALLBACK
			fullPath := filepath.Join(configDir, ri.Resolved)
			if _, err := os.Stat(fullPath); err != nil {
				return nil, fmt.Errorf("item %q (resolved to %q) does not exist for platform %s", item, ri.Resolved, platform)
			}
		} else {
			// Literal item - no resolution needed
			ri.Resolved = item
			ri.PackPath = item
		}

		resolved = append(resolved, ri)
	}
	return resolved, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run TestResolveItems -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/packager.go cmd/onepkg/packager_test.go
git commit -m "feat(onepkg): add ResolveItems function for platform marker resolution"
```

---

### Task 4: Update CollectFiles to Use Resolved Items

**Files:**
- Modify: `cmd/onepkg/packager.go`
- Modify: `cmd/onepkg/packager_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/onepkg/packager_test.go`:

```go
func TestCollectFilesWithResolvedItems(t *testing.T) {
	dir := t.TempDir()

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	etcDir := filepath.Join(dir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Platform-specific binary
	if err := os.WriteFile(filepath.Join(binDir, "myapp.linux-arm64"), []byte("binary-linux"), 0755); err != nil {
		t.Fatal(err)
	}
	// Literal config file
	if err := os.WriteFile(filepath.Join(etcDir, "config.conf"), []byte("config-data"), 0644); err != nil {
		t.Fatal(err)
	}

	resolved := []ResolvedItem{
		{Original: "bin/myapp.{platform}", Resolved: "bin/myapp.linux-arm64", PackPath: "bin/myapp"},
		{Original: "etc/config.conf", Resolved: "etc/config.conf", PackPath: "etc/config.conf"},
	}

	entries, err := CollectFilesResolved(dir, resolved)
	if err != nil {
		t.Fatalf("CollectFilesResolved returned error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	// Check that RelPath uses PackPath (marker stripped)
	pathMap := make(map[string]*FileEntry)
	for i := range entries {
		pathMap[entries[i].RelPath] = &entries[i]
	}

	// Should be "bin/myapp" (stripped), NOT "bin/myapp.linux-arm64"
	if _, ok := pathMap["bin/myapp"]; !ok {
		t.Error("missing bin/myapp (should have PackPath, not resolved path)")
	}
	if _, ok := pathMap["etc/config.conf"]; !ok {
		t.Error("missing etc/config.conf")
	}

	// Should NOT have the platform-suffixed path in RelPath
	if _, ok := pathMap["bin/myapp.linux-arm64"]; ok {
		t.Error("should not have bin/myapp.linux-arm64 in RelPath - marker should be stripped")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run TestCollectFilesWithResolvedItems -v`
Expected: FAIL — `undefined: CollectFilesResolved`

- [ ] **Step 3: Write CollectFilesResolved function**

Modify `cmd/onepkg/packager.go`, add the CollectFilesResolved function:

```go
// CollectFilesResolved collects files from resolved items, using PackPath as the package path
// This handles {platform} marker resolution: files are read from Resolved path but packed as PackPath
func CollectFilesResolved(configDir string, items []ResolvedItem) ([]FileEntry, error) {
	var entries []FileEntry
	for _, item := range items {
		cleanResolved := strings.TrimRight(item.Resolved, "/")
		fullPath := filepath.Join(configDir, cleanResolved)

		info, err := os.Stat(fullPath)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", item.Resolved, err)
		}

		if info.IsDir() {
			err = filepath.WalkDir(fullPath, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() {
					return nil
				}
				// Calculate relative path from the resolved directory, then prefix with PackPath
				relInDir, err := filepath.Rel(fullPath, path)
				if err != nil {
					return err
				}
				// Use PackPath as the base, preserving subdirectory structure
				cleanPackPath := strings.TrimRight(item.PackPath, "/")
				packRelPath := filepath.Join(cleanPackPath, relInDir)

				entry, err := makeFileEntryWithPackPath(configDir, path, packRelPath)
				if err != nil {
					return err
				}
				entries = append(entries, entry)
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("walk %s: %w", item.Resolved, err)
			}
		} else {
			entry, err := makeFileEntryWithPackPath(configDir, fullPath, item.PackPath)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func makeFileEntryWithPackPath(configDir, absPath, packRelPath string) (FileEntry, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return FileEntry{}, fmt.Errorf("read %s: %w", packRelPath, err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return FileEntry{}, fmt.Errorf("stat %s: %w", packRelPath, err)
	}
	hash := sha1.Sum(data)
	return FileEntry{
		RelPath: packRelPath,
		AbsPath: absPath,
		SHA1:    hex.EncodeToString(hash[:]),
		Mode:    info.Mode(),
		Size:    info.Size(),
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run TestCollectFilesWithResolvedItems -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/packager.go cmd/onepkg/packager_test.go
git commit -m "feat(onepkg): add CollectFilesResolved for platform-resolved item collection"
```

---

### Task 5: Update Config Validation for Platform Markers

**Files:**
- Modify: `cmd/onepkg/config.go`
- Modify: `cmd/onepkg/config_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/onepkg/config_test.go` in the `TestValidate` table:

```go
{
	name: "platform marker in non-existent file",
	cfg: &Config{
		Name: "myapp", Version: "v1.0.0",
		Items: []string{"bin/myapp.{platform}"},
	},
	wantErr: "does not exist", // {platform} markers are validated at build time, not config time
},
{
	name: "platform marker allowed syntax",
	cfg: &Config{
		Name: "myapp", Version: "v1.0.0",
		Items: []string{"bin/myapp"},
	},
	wantErr: "",
},
```

Actually, the validation should NOT check {platform} items at config validation time since we don't know the platform yet. Let me verify the current validation behavior handles this correctly.

- [ ] **Step 2: Run existing tests to understand behavior**

Run: `cd cmd/onepkg && go test -run TestValidate -v`
Expected: Current behavior - Validate checks if literal path exists. Items with `{platform}` marker would fail validation since `bin/myapp.{platform}` doesn't exist as a literal file.

- [ ] **Step 3: Update validation to skip {platform} items**

The validation should skip items with `{platform}` markers since they'll be validated at build time with the actual platform.

Modify the validation loop in `cmd/onepkg/config.go`:

```go
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
		if filepath.IsAbs(item) {
			return fmt.Errorf("absolute path not allowed in item %q", item)
		}
		cleanRel, err := filepath.Rel(configDir, filepath.Join(configDir, item))
		if err != nil || strings.HasPrefix(cleanRel, "..") {
			return fmt.Errorf("path traversal not allowed in item %q", item)
		}
		// Skip file existence check for {platform} markers - validated at build time
		if !HasPlatformMarker(item) {
			fullPath := filepath.Join(configDir, item)
			if _, err := os.Stat(fullPath); err != nil {
				return fmt.Errorf("item %q does not exist", item)
			}
		}
	}
	// Service validation (unchanged)
	itemSet := make(map[string]bool, len(c.Items))
	for _, item := range c.Items {
		itemSet[item] = true
	}
	for _, svc := range c.Services {
		if svc.Name != "" && !isValidName(svc.Name) {
			return fmt.Errorf("service name %q must contain only alphanumeric characters, hyphens, and underscores", svc.Name)
		}
		// Service exec validation needs to handle {platform} markers
		if svc.Exec != "" && !itemSet[svc.Exec] {
			// Check if exec matches a platform-marked item (stripped form)
			matched := false
			for _, item := range c.Items {
				if HasPlatformMarker(item) {
					if StripPlatformMarker(item) == svc.Exec {
						matched = true
						break
					}
				} else if strings.HasPrefix(svc.Exec, item) {
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

- [ ] **Step 4: Write tests for new validation behavior**

Append to `cmd/onepkg/config_test.go`:

```go
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
```

- [ ] **Step 5: Run all tests**

Run: `cd cmd/onepkg && go test -run "TestValidate" -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/onepkg/config.go cmd/onepkg/config_test.go
git commit -m "feat(onepkg): update validation to handle {platform} markers"
```

---

### Task 6: Add Platform Validation Function

**Files:**
- Create: `cmd/onepkg/platform.go`
- Create: `cmd/onepkg/platform_test.go`

- [ ] **Step 1: Write the failing tests**

`cmd/onepkg/platform_test.go`:

```go
package main

import (
	"testing"
)

func TestValidatePlatform(t *testing.T) {
	tests := []struct {
		platform string
		wantErr  string
	}{
		{"linux-arm64", ""},
		{"linux-amd64", ""},
		{"linux-x86_64", "invalid arch"}, // Should use amd64, not x86_64
		{"darwin-arm64", ""},
		{"darwin-amd64", ""},
		{"darwin-x86_64", "invalid arch"},
		{"windows-amd64", "invalid os"}, // MVP only supports linux/darwin
		{"linux", "invalid platform format"},
		{"arm64", "invalid platform format"},
		{"", "invalid platform format"},
		{"linux-arm65", "invalid arch"}, // Typo
		{"linuxo-arm64", "invalid os"},  // Typo
	}

	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			err := ValidatePlatform(tt.platform)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidatePlatform(%q) returned unexpected error: %v", tt.platform, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidatePlatform(%q) returned nil, want error containing %q", tt.platform, tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("ValidatePlatform(%q) error = %q, want error containing %q", tt.platform, err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestParsePlatforms(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
		wantErr  string
	}{
		{"linux-arm64", []string{"linux-arm64"}, ""},
		{"linux-arm64,linux-amd64", []string{"linux-arm64", "linux-amd64"}, ""},
		{"darwin-arm64,darwin-amd64", []string{"darwin-arm64", "darwin-amd64"}, ""},
		{"linux-arm64,invalid-platform", nil, "invalid platform"},
		{"", nil, "platform is required"},
		{"linux-arm64,", nil, "invalid platform format"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParsePlatforms(tt.input)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ParsePlatforms(%q) returned unexpected error: %v", tt.input, err)
				}
				if len(result) != len(tt.expected) {
					t.Errorf("ParsePlatforms(%q) returned %d platforms, want %d", tt.input, len(result), len(tt.expected))
				}
				for i, p := range tt.expected {
					if result[i] != p {
						t.Errorf("ParsePlatforms(%q)[%d] = %q, want %q", tt.input, i, result[i], p)
					}
				}
			} else {
				if err == nil {
					t.Errorf("ParsePlatforms(%q) returned nil error, want error containing %q", tt.input, tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("ParsePlatforms(%q) error = %q, want error containing %q", tt.input, err.Error(), tt.wantErr)
				}
			}
		})
	}
}
```

Add `"strings"` to imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run "TestValidatePlatform|TestParsePlatforms" -v`
Expected: FAIL — `undefined: ValidatePlatform`, `undefined: ParsePlatforms`

- [ ] **Step 3: Write the platform validation functions**

`cmd/onepkg/platform.go`:

```go
package main

import (
	"fmt"
	"strings"
)

var validOS = map[string]bool{
	"linux":  true,
	"darwin": true,
	// "windows": true, // Post-MVP
}

var validArch = map[string]bool{
	"amd64": true,
	"arm64": true,
	// "x86":    true, // Post-MVP
}

// ValidatePlatform checks that a platform string is valid format {os}-{arch}
func ValidatePlatform(platform string) error {
	if platform == "" {
		return fmt.Errorf("invalid platform format: empty string")
	}

	parts := strings.SplitN(platform, "-", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid platform format %q, expected {os}-{arch}", platform)
	}

	os, arch := parts[0], parts[1]

	if !validOS[os] {
		return fmt.Errorf("invalid os %q in platform %q (supported: linux, darwin)", os, platform)
	}

	if !validArch[arch] {
		return fmt.Errorf("invalid arch %q in platform %q (supported: amd64, arm64)", arch, platform)
	}

	return nil
}

// ParsePlatforms parses a comma-separated platform string into a list
// Validates each platform and returns error if any are invalid
func ParsePlatforms(input string) ([]string, error) {
	if input == "" {
		return nil, fmt.Errorf("platform is required")
	}

	platforms := strings.Split(input, ",")

	// Validate each platform
	for _, p := range platforms {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, fmt.Errorf("invalid platform format: empty platform in list")
		}
		if err := ValidatePlatform(p); err != nil {
			return nil, err
		}
	}

	// Trim whitespace from result
	result := make([]string, len(platforms))
	for i, p := range platforms {
		result[i] = strings.TrimSpace(p)
	}

	return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run "TestValidatePlatform|TestParsePlatforms" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/platform.go cmd/onepkg/platform_test.go
git commit -m "feat(onepkg): add platform validation with comma-separated parsing"
```

---

### Task 7: Update CLI to Make --platform Required

**Files:**
- Modify: `cmd/onepkg/main.go`
- Modify: `cmd/onepkg/main_test.go`

- [ ] **Step 1: Write the failing test**

Modify `cmd/onepkg/main_test.go` to require --platform:

```go
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

	// Create platform-specific binary
	if err := os.WriteFile(filepath.Join(binDir, "app.linux-amd64"), []byte("#!/bin/sh\necho hello"), 0755); err != nil {
		t.Fatal(err)
	}
	// Create literal config file
	if err := os.WriteFile(filepath.Join(etcDir, "app.conf"), []byte("key=value"), 0644); err != nil {
		t.Fatal(err)
	}

	configContent := `
name = "testapp"
version = "v1.0.0"
output_dir = "packages"

items = [
  "bin/app.{platform}",
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

	// Platforms must be provided via CLI, not config
	platforms := []string{"linux-amd64"}
	cfg.Platforms = platforms

	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// Resolve items for platform
	resolved, err := ResolveItems(projectDir, cfg.Items, "linux-amd64")
	if err != nil {
		t.Fatalf("ResolveItems: %v", err)
	}

	entries, err := CollectFilesResolved(projectDir, resolved)
	if err != nil {
		t.Fatalf("CollectFilesResolved: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	// Verify RelPath uses stripped marker path
	pathMap := make(map[string]bool)
	for _, e := range entries {
		pathMap[e.RelPath] = true
	}
	if !pathMap["bin/app"] {
		t.Error("missing bin/app (should have stripped {platform} marker)")
	}
	if !pathMap["etc/app.conf"] {
		t.Error("missing etc/app.conf")
	}

	tarData, err := BuildTarGZ(entries)
	if err != nil {
		t.Fatalf("BuildTarGZ: %v", err)
	}

	shaMap := make(map[string]string)
	for _, e := range entries {
		shaMap[e.RelPath] = e.SHA1
	}

	platform := platforms[0]
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
	// Should have "bin/app" (stripped), not "bin/app.linux-amd64"
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

- [ ] **Step 2: Run test to verify current behavior**

Run: `cd cmd/onepkg && go test -run TestBuildPackage -v`
Expected: Current test may pass or fail depending on existing behavior

- [ ] **Step 3: Update main.go to make --platform required**

Modify `cmd/onepkg/main.go`:

```go
func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	var configPath string
	var platformInput string
	var outputOverride string

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-c", "--config":
			if i+1 < len(os.Args) {
				configPath = os.Args[i+1]
				i++
			}
		case "--platform":
			if i+1 < len(os.Args) {
				platformInput = os.Args[i+1]
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

	// Platform is now required
	if platformInput == "" {
		fmt.Println("Error: --platform <platforms> is required (e.g., --platform linux-arm64 or --platform linux-arm64,linux-amd64)")
		os.Exit(1)
	}

	platforms, err := ParsePlatforms(platformInput)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	cfg, err := ParseConfig(configPath)
	if err != nil {
		fmt.Printf("Error parsing config: %v\n", err)
		os.Exit(1)
	}

	cfg.ApplyDefaults()

	cfg.Platforms = platforms
	if outputOverride != "" {
		cfg.OutputDir = outputOverride
	}

	configDir := filepath.Dir(configPath)
	if err := cfg.Validate(configDir); err != nil {
		fmt.Printf("Error validating config: %v\n", err)
		os.Exit(1)
	}

	outputDir := filepath.Join(configDir, cfg.OutputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Printf("Error creating output dir: %v\n", err)
		os.Exit(1)
	}

	for _, platform := range cfg.Platforms {
		// Resolve items for this platform
		resolved, err := ResolveItems(configDir, cfg.Items, platform)
		if err != nil {
			fmt.Printf("Error resolving items for %s: %v\n", platform, err)
			os.Exit(1)
		}

		entries, err := CollectFilesResolved(configDir, resolved)
		if err != nil {
			fmt.Printf("Error collecting files for %s: %v\n", platform, err)
			os.Exit(1)
		}

		tarData, err := BuildTarGZ(entries)
		if err != nil {
			fmt.Printf("Error building archive for %s: %v\n", platform, err)
			os.Exit(1)
		}

		shaMap := make(map[string]string)
		for _, e := range entries {
			shaMap[e.RelPath] = e.SHA1
		}

		if err := BuildForPlatform(cfg, platform, entries, tarData, shaMap, outputDir); err != nil {
			fmt.Printf("Error building %s: %v\n", platform, err)
			os.Exit(1)
		}
		fmt.Printf("Created: %s\n", platformOutputPath(cfg, platform, outputDir))
	}

	fmt.Printf("\nBuilt %d platform(s)\n", len(cfg.Platforms))
}
```

- [ ] **Step 4: Update help text**

Modify `printHelp` in `cmd/onepkg/main.go`:

```go
func printHelp() {
	fmt.Println(`onepkg - General-purpose self-extracting installer generator

Usage:
  onepkg -c config.toml --platform <platforms> [options]

Options:
  -c, --config <path>      Path to config.toml (required)
      --platform <p>       Target platforms, comma-separated (required)
                           e.g., --platform linux-arm64
                           e.g., --platform linux-arm64,linux-amd64
      --output <dir>       Override output directory
  -h, --help               Show this help

Supported platforms:
  linux-amd64, linux-arm64
  darwin-amd64, darwin-arm64

Item syntax:
  Items can use {platform} marker for platform-specific files:
    "bin/myapp.{platform}"  -> resolves to bin/myapp.linux-arm64, packed as bin/myapp
  Literal items are unchanged:
    "etc/config.conf"       -> packed as etc/config.conf

Examples:
  onepkg -c config.toml --platform linux-arm64
  onepkg -c config.toml --platform linux-arm64,linux-amd64
  onepkg -c config.toml --platform darwin-arm64 --output ./dist`)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run TestBuildPackage -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/onepkg/main.go cmd/onepkg/main_test.go
git commit -m "feat(onepkg): make --platform required with comma-separated multi-platform support"
```

---

### Task 8: Update Spec Document

**Files:**
- Modify: `docs/superpowers/specs/2026-05-25-onepkg-mvp-design.md`

- [ ] **Step 1: Update the spec document**

Update the Config Schema section to remove platforms and document {platform} marker:

```markdown
## Config Schema (config.toml)

```toml
name = "autotest"
version = "v1.0.0"
output_dir = "packages"

items = [
  "bin/test1.{platform}",   # Platform-specific binary
  "bin/helper.{platform}",  # Another platform-specific binary
  "etc/gate.conf",          # Literal file (no platform suffix)
  "data/",                  # Directory (no platform suffix)
  "res/",
]

[[services]]
name = "main"          # default: package name
exec = "bin/test1"     # Use stripped path (without {platform})
restart = "on-failure" # no | on-failure | always
user = "autotest"      # default: root

[services.linux]
type = "simple"
after = ["network.target"]
restart_sec = 5

[services.macos]
run_at_load = true
keep_alive = true
```
```

And update the CLI Usage section:

```markdown
## CLI Usage

```bash
# Build for single platform (required)
onepkg -c config.toml --platform linux-arm64

# Build for multiple platforms (comma-separated)
onepkg -c config.toml --platform linux-arm64,linux-amd64

# Specify output directory
onepkg -c config.toml --platform linux-arm64 --output ./dist
```

## Platform Resolution

Items with `{platform}` marker are resolved at build time:

| Config Item | Platform | Resolved Source File | Pack Path |
|-------------|----------|---------------------|-----------|
| `bin/myapp.{platform}` | linux-arm64 | `bin/myapp.linux-arm64` | `bin/myapp` |
| `bin/helper.{platform}` | darwin-amd64 | `bin/helper.darwin-amd64` | `bin/helper` |
| `etc/config.conf` | any | `etc/config.conf` | `etc/config.conf` |

**Failure behavior:** If a platform-specific file doesn't exist, the build fails immediately. No fallback to base filename.
```

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/specs/2026-05-25-onepkg-mvp-design.md
git commit -m "docs: update spec for platform resolution and required --platform"
```

---

### Task 9: Run Full Test Suite

**Files:**
- None (verification)

- [ ] **Step 1: Run all tests**

Run: `cd cmd/onepkg && go test ./... -v`
Expected: ALL PASS

- [ ] **Step 2: Build and verify CLI**

Run: `cd cmd/onepkg && go build -o onepkg .`
Expected: Success (no errors)

- [ ] **Step 3: Test CLI error messages**

Run: `cd cmd/onepkg && ./onepkg -c nonexistent.toml --platform linux-arm64`
Expected: Error message about missing config

Run: `cd cmd/onepkg && ./onepkg -c config.toml`
Expected: Error message about missing --platform

Run: `cd cmd/onepkg && ./onepkg -c config.toml --platform invalid`
Expected: Error message about invalid platform format

- [ ] **Step 4: Commit if any fixes needed**

If any fixes were needed during verification, commit them.

---

## Self-Review

### 1. Spec Coverage

| Design Requirement | Task |
|---|---|
| Remove `platforms` from config.toml | Task 1 |
| Make `--platform` flag required | Task 7 |
| Support comma-separated multiple platforms | Task 6, Task 7 |
| `{platform}` marker in item paths | Task 2 |
| Resolve `{platform}` to actual files | Task 3 |
| Rename resolved files to base path in package | Task 4 |
| Fail fast if platform file missing (no fallback) | Task 3 (TestResolveItemsMissingPlatformFile, TestResolveItemsNoFallback) |
| Multiple platforms = multiple output files | Task 7 |
| Validate platform format early | Task 6 |

### 2. Placeholder Scan

No TBD, TODO, or placeholder patterns found. All steps contain complete code.

### 3. Type Consistency

- `ResolvedItem` defined in Task 3, used in Task 4, Task 7 — consistent fields (`Original`, `Resolved`, `PackPath`)
- `HasPlatformMarker`, `ResolvePlatformItem`, `StripPlatformMarker` defined in Task 2, used in Task 3, Task 5 — consistent signatures
- `ValidatePlatform`, `ParsePlatforms` defined in Task 6, used in Task 7 — consistent signatures
- `CollectFilesResolved` defined in Task 4, used in Task 7 — consistent signature with `[]ResolvedItem` input