# Windows `.exe` Extension for Bundled Executables — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On Windows builds, pack executed bundled items (those referenced by `commands[].exec` or `services[].exec`) with an `.exe` extension and point the generated PowerShell at those `.exe` paths, so Windows can launch them.

**Architecture:** Three pure helpers in `packager.go` (`execTargetPaths`, `applyExeExtension`, `resolveWindowsItemPath`) plus one pipeline entry point (`ResolveItemsForPlatform`) that folds the existing `ResolveItems` → `CollectFilesResolved` steps and inserts the Windows-only `.exe` pass between them. `main()`'s per-platform loop and the build tests both call `ResolveItemsForPlatform`, so the `.exe` wiring lives in one place. The Windows build functions resolve `commands[].exec`, `services[].exec`, and `system_overlay[].item` against the packed entries so the generated PowerShell addresses the real `.exe` path.

**Tech Stack:** Go 1.21, module `onepkg`, package `main` in `cmd/onepkg/`. No new dependencies.

## Global Constraints

Copied verbatim from the approved spec (`docs/superpowers/specs/2026-07-14-windows-exe-extension-design.md`):

- `.exe` is appended **only** on `windows-*` builds, **only** to items that are exec targets (`commands[].exec` or `services[].exec`), and **only** when the item's pack path has no file extension (`filepath.Ext(packPath) == ""`).
- Non-executed items and items that already carry an extension (`.exe`, `.ps1`, `.conf`, …) are never re-extended (idempotent).
- Linux/macOS builds are byte-identical to today (the `.exe` pass is gated on `goos == "windows"`).
- The author's **source** artifact is looked up unchanged (`bin/onepkg.{platform}` → `bin/onepkg.windows-amd64`); only the in-package (zip) name gains `.exe`.
- Single service only: use `cfg.Services[0]` (MVP rule).
- Generated PowerShell style is unchanged (still `& (Join-Path $PayloadDir '<path>')` for run, `$SERVICE_EXEC` / `$OVERLAY_n_ITEM` for install).
- Do not modify `build-package/` (symlink to another project).
- TDD: write the failing test first, run it to confirm it fails, implement, run it to confirm it passes, then commit.
- Windows stubs (`cmd/onepkg/installer/stubs/*.exe`) are gitignored and required by `//go:embed`. Before running any test, run `make build` (or `make win-wrapper`) once, or simply `make test` (which regenerates stubs then runs both suites). Individual-package test runs need pre-existing stubs.

---

## File Structure

| File | Responsibility | Touched by |
|------|----------------|------------|
| `cmd/onepkg/packager.go` | New pure helpers + the `ResolveItemsForPlatform` pipeline entry point. Existing `ResolveItems`/`CollectFilesResolved` unchanged. | Tasks 1, 2 |
| `cmd/onepkg/main.go` | `main()` loop calls `ResolveItemsForPlatform`; `BuildForWindowsRun` resolves command exec; `BuildForWindows` resolves service exec + overlay item. | Task 2 |
| `cmd/onepkg/packager_test.go` | Pure unit tests for the three helpers. | Task 1 |
| `cmd/onepkg/main_test.go` | Run-mode + install-mode integration tests; Linux/macOS and non-exec-item regressions. | Task 2 |
| `docs/config-example.toml` | Comment noting the Windows auto-`.exe` behavior. | Task 3 |
| `CLAUDE.md` | Convention bullet for the Windows auto-`.exe` behavior. | Task 3 |

---

## Task 1: Pure helpers in `packager.go`

**Files:**
- Modify: `cmd/onepkg/packager.go` (append three functions after `ResolveItems`, which ends at the `}` closing `func ResolveItems` — currently around line 59).
- Test: `cmd/onepkg/packager_test.go` (append new tests).

**Interfaces:**
- Consumes: existing types `Config` (`cmd/onepkg/config.go:14`), `CommandConfig` (`config.go:32`), `ServiceConfig` (`config.go:38`), `ResolvedItem` (`packager.go`), `FileEntry` (`packager.go`). Already in package `main`.
- Produces (exact signatures, used by Task 2):
  - `func execTargetPaths(cfg *Config) map[string]bool`
  - `func applyExeExtension(resolved []ResolvedItem, execTargets map[string]bool) []ResolvedItem`
  - `func resolveWindowsItemPath(entries []FileEntry, ref string) string`

- [ ] **Step 1: Write the failing tests** in `cmd/onepkg/packager_test.go`

Append to the end of `packager_test.go`:

```go
func TestExecTargetPaths(t *testing.T) {
	cfg := &Config{
		Commands: []CommandConfig{
			{Exec: "bin/onepkg"},
			{Shell: "Write-Host hi"}, // no exec -> ignored
			{Exec: "bin/helper"},
		},
		Services: []ServiceConfig{{Exec: "bin/svc"}},
	}
	got := execTargetPaths(cfg)
	want := map[string]bool{"bin/onepkg": true, "bin/helper": true, "bin/svc": true}
	if len(got) != len(want) {
		t.Fatalf("len(targets) = %d, want %d; got %v", len(got), len(want), got)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("missing exec target %q; got %v", k, got)
		}
	}
}

func TestExecTargetPathsEmpty(t *testing.T) {
	if got := execTargetPaths(&Config{}); len(got) != 0 {
		t.Errorf("expected empty target set for empty config; got %v", got)
	}
}

func TestApplyExeExtension(t *testing.T) {
	targets := map[string]bool{"bin/onepkg": true, "bin/tool": true, "bin/myapp.exe": true}
	resolved := []ResolvedItem{
		{Original: "bin/onepkg.{platform}", Resolved: "bin/onepkg.windows-amd64", PackPath: "bin/onepkg"},       // exec target, extensionless -> .exe
		{Original: "bin/tool", Resolved: "bin/tool", PackPath: "bin/tool"},                                        // literal exec target, extensionless -> .exe
		{Original: "data/README", Resolved: "data/README", PackPath: "data/README"},                                // not an exec target -> unchanged
		{Original: "bin/myapp.{platform}.exe", Resolved: "bin/myapp.windows-amd64.exe", PackPath: "bin/myapp.exe"}, // exec target, already has extension -> unchanged (idempotent)
		{Original: "scripts/setup.ps1", Resolved: "scripts/setup.ps1", PackPath: "scripts/setup.ps1"},             // other extension -> unchanged
	}
	got := applyExeExtension(resolved, targets)
	want := []string{"bin/onepkg.exe", "bin/tool.exe", "data/README", "bin/myapp.exe", "scripts/setup.ps1"}
	for i, w := range want {
		if got[i].PackPath != w {
			t.Errorf("item %d PackPath = %q, want %q", i, got[i].PackPath, w)
		}
	}
}

func TestResolveWindowsItemPath(t *testing.T) {
	entries := []FileEntry{
		{RelPath: "bin/onepkg.exe"},
		{RelPath: "etc/config.conf"},
	}
	tests := []struct{ ref, want string }{
		{"bin/onepkg", "bin/onepkg.exe"},     // only the .exe form is packed -> resolve to it
		{"etc/config.conf", "etc/config.conf"}, // exact match
		{"bin/missing", "bin/missing"},        // neither form present -> unchanged (fallback)
		{"", ""},                              // empty ref -> empty
	}
	for _, tt := range tests {
		if got := resolveWindowsItemPath(entries, tt.ref); got != tt.want {
			t.Errorf("resolveWindowsItemPath(%q) = %q, want %q", tt.ref, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd cmd/onepkg && go test -run 'TestExecTargetPaths|TestApplyExeExtension|TestResolveWindowsItemPath' -v`
Expected: FAIL / build error — `execTargetPaths undefined`, `applyExeExtension undefined`, `resolveWindowsItemPath undefined`.

- [ ] **Step 3: Implement the three helpers** in `cmd/onepkg/packager.go`

Append immediately after the closing brace of `func ResolveItems` (after the `return resolved, nil` block):

```go
// execTargetPaths returns the set of config-form exec references that name
// bundled items: every non-empty commands[].exec plus the single service exec
// (cfg.Services[0].Exec). The keys are identical to an item's stripped pack path
// (e.g. "bin/onepkg"), so applyExeExtension can match items to execs directly.
func execTargetPaths(cfg *Config) map[string]bool {
	targets := make(map[string]bool)
	for _, c := range cfg.Commands {
		if c.Exec != "" {
			targets[c.Exec] = true
		}
	}
	if len(cfg.Services) > 0 && cfg.Services[0].Exec != "" {
		targets[cfg.Services[0].Exec] = true
	}
	return targets
}

// applyExeExtension appends ".exe" to the pack path of any resolved item that is
// both an exec target and extensionless. It never touches items that already have
// a file extension or that are not referenced by an exec. This is the Windows-only
// pass that lets an extensionless {platform} binary (packed as "bin/onepkg") be
// launched by Windows (as "bin/onepkg.exe"). The match uses the pre-extension
// PackPath, which equals the config-form exec string that itemMatches validates.
func applyExeExtension(resolved []ResolvedItem, execTargets map[string]bool) []ResolvedItem {
	for i := range resolved {
		ri := &resolved[i]
		if execTargets[ri.PackPath] && filepath.Ext(ri.PackPath) == "" {
			ri.PackPath += ".exe"
		}
	}
	return resolved
}

// resolveWindowsItemPath maps a config reference (a command/service exec, or an
// overlay item) to its actual packed path. It returns ref unchanged when an entry
// matches exactly (already-extensioned or non-extended refs); otherwise ref+".exe"
// when that matches (the Windows auto-extension); otherwise ref unchanged as a
// defensive fallback. With a valid config the fallback is unreachable for exec and
// overlay refs.
func resolveWindowsItemPath(entries []FileEntry, ref string) string {
	if ref == "" {
		return ""
	}
	for _, e := range entries {
		if e.RelPath == ref {
			return ref
		}
	}
	withExe := ref + ".exe"
	for _, e := range entries {
		if e.RelPath == withExe {
			return withExe
		}
	}
	return ref
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd cmd/onepkg && go test -run 'TestExecTargetPaths|TestApplyExeExtension|TestResolveWindowsItemPath' -v`
Expected: PASS — all four test functions pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/packager.go cmd/onepkg/packager_test.go
git commit -m "feat(packager): add Windows .exe extension helpers

execTargetPaths, applyExeExtension, resolveWindowsItemPath — pure helpers
for packing executed extensionless items with .exe on Windows."
```

---

## Task 2: Wire the `.exe` extension through the Windows build path

**Files:**
- Modify: `cmd/onepkg/packager.go` (add `ResolveItemsForPlatform` after the helpers from Task 1).
- Modify: `cmd/onepkg/main.go` (the per-platform loop ~line 128; `BuildForWindowsRun` ~line 468; `BuildForWindows` ~line 398 and ~line 417).
- Test: `cmd/onepkg/main_test.go` (append new tests).

**Interfaces:**
- Consumes: Task 1's `execTargetPaths`, `applyExeExtension`, `resolveWindowsItemPath`; existing `ResolveItems`, `CollectFilesResolved`, `BuildForPlatform`/`BuildForWindows`/`BuildForWindowsRun`, and `installer.CommandSpec`/`installer.OverlaySpec`/`installer.WindowsServiceConfig`.
- Produces: `func ResolveItemsForPlatform(configDir string, cfg *Config, platform string) ([]FileEntry, error)` — used by `main()` and the build tests.

- [ ] **Step 1: Write the failing integration tests** in `cmd/onepkg/main_test.go`

Append to the end of `main_test.go` (the file already imports `archive/zip` is not needed; it imports `bytes`, `os`, `path/filepath`, `strings`, `testing` — all present):

```go
func TestWindowsRunAutoExeExtension(t *testing.T) {
	projectDir := t.TempDir()
	binDir := filepath.Join(projectDir, "bin")
	os.MkdirAll(binDir, 0755)
	// Source artifact is extensionless, matching {platform} resolution.
	os.WriteFile(filepath.Join(binDir, "onepkg.windows-amd64"), []byte("fake-exe"), 0755)

	configContent := `
name = "rustdesk-config"
version = "v1.0.0"
mode = "run"
items = ["bin/onepkg.{platform}"]

[[commands]]
exec = "bin/onepkg"
args = ["--version"]
`
	configPath := filepath.Join(projectDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	platform := "windows-amd64"
	entries, err := ResolveItemsForPlatform(projectDir, cfg, platform)
	if err != nil {
		t.Fatalf("ResolveItemsForPlatform: %v", err)
	}

	// The executed item must be packed as bin/onepkg.exe, never bin/onepkg.
	var sawExe bool
	for _, e := range entries {
		if e.RelPath == "bin/onepkg.exe" {
			sawExe = true
		}
		if e.RelPath == "bin/onepkg" {
			t.Error("extensionless bin/onepkg must not be packed; expected bin/onepkg.exe")
		}
	}
	if !sawExe {
		t.Fatalf("entries missing bin/onepkg.exe; RelPaths: %v", relPathsOf(entries))
	}

	outputDir := filepath.Join(projectDir, "packages")
	os.MkdirAll(outputDir, 0755)
	if err := BuildForPlatform(cfg, platform, entries, nil, nil, outputDir); err != nil {
		t.Fatalf("BuildForPlatform: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "rustdesk-config_v1.0.0_windows_amd64.exe"))
	if err != nil {
		t.Fatalf("read exe: %v", err)
	}
	// run.ps1 must invoke the .exe path, never the extensionless one.
	if !bytes.Contains(data, []byte("Join-Path $PayloadDir 'bin/onepkg.exe'")) {
		t.Error("embedded run.ps1 missing 'Join-Path $PayloadDir 'bin/onepkg.exe''")
	}
	if bytes.Contains(data, []byte("Join-Path $PayloadDir 'bin/onepkg'")) {
		t.Error("embedded run.ps1 references extensionless bin/onepkg")
	}
}

func TestWindowsRunNonExecItemUnchanged(t *testing.T) {
	projectDir := t.TempDir()
	binDir := filepath.Join(projectDir, "bin")
	dataDir := filepath.Join(projectDir, "data")
	os.MkdirAll(binDir, 0755)
	os.MkdirAll(dataDir, 0755)
	os.WriteFile(filepath.Join(binDir, "onepkg.windows-amd64"), []byte("fake-exe"), 0755)
	os.WriteFile(filepath.Join(dataDir, "README"), []byte("readme"), 0644)

	cfg := &Config{
		Name:    "x",
		Version: "v1",
		Mode:    "run",
		Items:   []string{"bin/onepkg.{platform}", "data/README"},
		Commands: []CommandConfig{{Exec: "bin/onepkg"}},
	}
	entries, err := ResolveItemsForPlatform(projectDir, cfg, "windows-amd64")
	if err != nil {
		t.Fatalf("ResolveItemsForPlatform: %v", err)
	}
	// bin/onepkg is an exec target -> .exe; data/README is not -> unchanged.
	for _, e := range entries {
		if e.RelPath == "data/README.exe" {
			t.Errorf("non-exec item must not gain .exe; got %q", e.RelPath)
		}
	}
}

func TestResolveItemsForPlatformLinuxNoExe(t *testing.T) {
	projectDir := t.TempDir()
	binDir := filepath.Join(projectDir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "onepkg.linux-arm64"), []byte("x"), 0755)

	cfg := &Config{
		Name:     "x",
		Version:  "v1",
		Items:    []string{"bin/onepkg.{platform}"},
		Commands: []CommandConfig{{Exec: "bin/onepkg"}}, // exec target, but Linux -> no .exe
	}
	entries, err := ResolveItemsForPlatform(projectDir, cfg, "linux-arm64")
	if err != nil {
		t.Fatalf("ResolveItemsForPlatform: %v", err)
	}
	for _, e := range entries {
		if e.RelPath == "bin/onepkg.exe" {
			t.Errorf("Linux build must not add .exe; got %q", e.RelPath)
		}
		if e.RelPath != "bin/onepkg" {
			t.Errorf("Linux pack path = %q, want bin/onepkg", e.RelPath)
		}
	}
}

func TestWindowsInstallAutoExeExtension(t *testing.T) {
	projectDir := t.TempDir()
	binDir := filepath.Join(projectDir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "onepkg.windows-amd64"), []byte("fake-exe"), 0755)

	// Service exec and an overlay both reference the extensionless {platform} item.
	// The overlay system path uses a {dst.} known folder so it is not Go-validated
	// for absoluteness on the macOS build host (only literal paths are).
	configContent := `
name = "svcapp"
version = "v1.0.0"
items = ["bin/onepkg.{platform}"]

[[services]]
name = "svcapp"
exec = "bin/onepkg"

[services.windows]
start_type = "auto"

[[system_overlay]]
system = "{dst.ProgramFiles64Folder}/OtherApp/conf"
item = "bin/onepkg"
`
	configPath := filepath.Join(projectDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	platform := "windows-amd64"
	entries, err := ResolveItemsForPlatform(projectDir, cfg, platform)
	if err != nil {
		t.Fatalf("ResolveItemsForPlatform: %v", err)
	}
	var sawExe bool
	for _, e := range entries {
		if e.RelPath == "bin/onepkg.exe" {
			sawExe = true
		}
	}
	if !sawExe {
		t.Fatalf("entries missing bin/onepkg.exe; RelPaths: %v", relPathsOf(entries))
	}

	shaMap := make(map[string]string)
	for _, e := range entries {
		shaMap[e.RelPath] = e.SHA1
	}
	outputDir := filepath.Join(projectDir, "packages")
	os.MkdirAll(outputDir, 0755)
	if err := BuildForPlatform(cfg, platform, entries, nil, shaMap, outputDir); err != nil {
		t.Fatalf("BuildForPlatform: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "svcapp_v1.0.0_windows_amd64.exe"))
	if err != nil {
		t.Fatalf("read exe: %v", err)
	}
	// Service exec and overlay item must both reference the .exe path.
	if !bytes.Contains(data, []byte("$SERVICE_EXEC = 'bin/onepkg.exe'")) {
		t.Error("install.ps1 missing $SERVICE_EXEC = 'bin/onepkg.exe'")
	}
	if !bytes.Contains(data, []byte("$OVERLAY_0_ITEM = 'bin/onepkg.exe'")) {
		t.Error("install.ps1 missing $OVERLAY_0_ITEM = 'bin/onepkg.exe'")
	}
}

// relPathsOf returns the RelPath values of entries, for assertion failure messages.
func relPathsOf(entries []FileEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.RelPath
	}
	return out
}
```

- [ ] **Step 2: Run the tests to verify they fail**

First ensure stubs exist: `make build` (run once from repo root).
Run: `cd cmd/onepkg && go test -run 'TestWindowsRunAutoExeExtension|TestWindowsRunNonExecItemUnchanged|TestResolveItemsForPlatformLinuxNoExe|TestWindowsInstallAutoExeExtension' -v`
Expected: FAIL / build error — `ResolveItemsForPlatform undefined` (and the behavior assertions fail once it compiles but is unwired).

- [ ] **Step 3: Add `ResolveItemsForPlatform`** in `cmd/onepkg/packager.go`

Append immediately after the `resolveWindowsItemPath` function added in Task 1:

```go
// ResolveItemsForPlatform is the single entry point for the per-platform item
// pipeline: it resolves {platform} markers, applies the Windows .exe extension to
// executed extensionless items, and collects the file entries. main()'s build loop
// and the build tests both use it so the .exe wiring lives in exactly one place.
func ResolveItemsForPlatform(configDir string, cfg *Config, platform string) ([]FileEntry, error) {
	resolved, err := ResolveItems(configDir, cfg.Items, platform)
	if err != nil {
		return nil, err
	}
	goos := strings.SplitN(platform, "-", 2)[0]
	if goos == "windows" {
		resolved = applyExeExtension(resolved, execTargetPaths(cfg))
	}
	return CollectFilesResolved(configDir, resolved, cfg.Exclude)
}
```

(`strings` and `filepath` are already imported in `packager.go`.)

- [ ] **Step 4: Wire `main()`'s per-platform loop** in `cmd/onepkg/main.go`

Replace this block (currently around lines 128-139):

```go
	for _, platform := range platforms {
		resolved, err := ResolveItems(configDir, cfg.Items, platform)
		if err != nil {
			fmt.Printf("Error resolving items for %s: %v\n", platform, err)
			os.Exit(1)
		}

		entries, err := CollectFilesResolved(configDir, resolved, cfg.Exclude)
		if err != nil {
			fmt.Printf("Error collecting files for %s: %v\n", platform, err)
			os.Exit(1)
		}
```

with:

```go
	for _, platform := range platforms {
		entries, err := ResolveItemsForPlatform(configDir, cfg, platform)
		if err != nil {
			fmt.Printf("Error resolving items for %s: %v\n", platform, err)
			os.Exit(1)
		}
```

This removes the now-unused local `resolved` in the loop (it was only ever passed to `CollectFilesResolved`).

- [ ] **Step 5: Resolve command exec in `BuildForWindowsRun`** in `cmd/onepkg/main.go`

In `BuildForWindowsRun` (the `commands` loop, currently around lines 468-475), replace:

```go
	commands := make([]installer.CommandSpec, 0, len(cfg.Commands))
	for _, c := range cfg.Commands {
		commands = append(commands, installer.CommandSpec{
			Exec:  c.Exec,
			Args:  c.Args,
			Shell: c.Shell,
		})
	}
```

with:

```go
	commands := make([]installer.CommandSpec, 0, len(cfg.Commands))
	for _, c := range cfg.Commands {
		commands = append(commands, installer.CommandSpec{
			Exec:  resolveWindowsItemPath(entries, c.Exec),
			Args:  c.Args,
			Shell: c.Shell,
		})
	}
```

- [ ] **Step 6: Resolve service exec and overlay item in `BuildForWindows`** in `cmd/onepkg/main.go`

In `BuildForWindows`, the service config block sets `Exec: svc.Exec` (currently around line 398). Replace:

```go
			// Exec is kept relative; install.ps1 resolves it against $InstallDir at
			// install time so a custom -InstallDir is reflected in the service binPath
			// (matching how the .sh installer rewrites DEFAULT_DIR -> install_dir).
			Exec: svc.Exec,
```

with:

```go
			// Exec is kept relative; install.ps1 resolves it against $InstallDir at
			// install time so a custom -InstallDir is reflected in the service binPath
			// (matching how the .sh installer rewrites DEFAULT_DIR -> install_dir).
			// resolveWindowsItemPath maps the config exec to its packed path, so an
			// extensionless {platform} item packed as .exe on Windows is addressed.
			Exec: resolveWindowsItemPath(entries, svc.Exec),
```

Then the overlay conversion (currently around lines 416-421). Replace:

```go
	for _, o := range cfg.SystemOverlay {
		overlays = append(overlays, installer.OverlaySpec{
			System: o.System,
			Item:   o.Item,
		})
	}
```

with:

```go
	for _, o := range cfg.SystemOverlay {
		overlays = append(overlays, installer.OverlaySpec{
			System: o.System,
			Item:   resolveWindowsItemPath(entries, o.Item),
		})
	}
```

- [ ] **Step 7: Run the new tests to verify they pass**

Run: `cd cmd/onepkg && go test -run 'TestWindowsRunAutoExeExtension|TestWindowsRunNonExecItemUnchanged|TestResolveItemsForPlatformLinuxNoExe|TestWindowsInstallAutoExeExtension' -v`
Expected: PASS — all four pass.

- [ ] **Step 8: Run the full test suite to confirm no regressions**

Run: `make test` (from repo root; regenerates Windows stubs, then runs the main + installer suites).
Expected: PASS — the pre-existing `TestBuildForWindows` and `TestBuildForWindowsRunWithItems` still pass (they use explicit `.exe` items, which the new code leaves unchanged), and everything else is green.

- [ ] **Step 9: Commit**

```bash
git add cmd/onepkg/packager.go cmd/onepkg/main.go cmd/onepkg/main_test.go
git commit -m "feat(windows): pack executed items with .exe and resolve exec refs

Windows builds auto-append .exe to extensionless items referenced by
commands[].exec / services[].exec, and run.ps1/install.ps1 address those
.exe paths. Fixes the 'How do you want to open this file?' prompt for
extensionless {platform} executables. Linux/macOS unchanged."
```

---

## Task 3: Document the Windows auto-`.exe` behavior

**Files:**
- Modify: `docs/config-example.toml` (the `{platform}` items comment block, ~line 31).
- Modify: `CLAUDE.md` (the "Key Conventions" section).

**Interfaces:** None (documentation only).

- [ ] **Step 1: Update the `{platform}` comment in `docs/config-example.toml`**

Find the platform-specific items comment (currently around lines 30-35):

```toml
# Platform-specific files: Use {platform} marker for binaries that vary by platform.
# The marker resolves to the actual platform string (e.g., linux-arm64).
# Example: "bin/myapp.{platform}" resolves to "bin/myapp.linux-arm64" for linux-arm64,
# and packs as "bin/myapp" (marker stripped) in the final package.
items = [
  "bin/myapp.{platform}",  # platform-specific binary (required for each --platform)
```

Replace the four comment lines (keep the `items = [` line and below unchanged) with:

```toml
# Platform-specific files: Use {platform} marker for binaries that vary by platform.
# The marker resolves to the actual platform string (e.g., linux-arm64).
# Example: "bin/myapp.{platform}" resolves to "bin/myapp.linux-arm64" for linux-arm64,
# and packs as "bin/myapp" (marker stripped) in the final package.
# On Windows, an item that is executed (commands[].exec / services[].exec) and has
# no file extension is packed with ".exe" appended automatically, so
# "bin/myapp.{platform}" packs as "bin/myapp.exe" on windows-amd64 (the exec still
# reads "bin/myapp"). Linux/macOS pack as "bin/myapp" with no extension.
```

- [ ] **Step 2: Add a convention bullet to `CLAUDE.md`**

In the "Key Conventions" section, add this bullet (e.g., after the "Platform resolution" bullet):

```markdown
- **Windows executable extension:** On Windows builds, an item referenced by `commands[].exec` or `services[].exec` is packed with `.exe` appended when its pack path has no extension (so `bin/myapp.{platform}` → `bin/myapp.exe` on Windows). The generated PowerShell addresses the `.exe` path. Non-executed items and already-extensioned items are untouched; Linux/macOS builds are unaffected. Implemented in `ResolveItemsForPlatform` / `applyExeExtension` / `resolveWindowsItemPath` (`packager.go`).
```

- [ ] **Step 3: Commit**

```bash
git add docs/config-example.toml CLAUDE.md
git commit -m "docs: document Windows auto-.exe extension for executed items"
```

---

## Self-Review

**Spec coverage:** Every spec requirement maps to a task step.
- "Executed items get `.exe` on Windows" → Task 1 `applyExeExtension` + Task 2 `ResolveItemsForPlatform` (windows gate). ✓
- "Non-executed items untouched" → Task 1 `TestApplyExeExtension` (data/README case) + Task 2 `TestWindowsRunNonExecItemUnchanged`. ✓
- "PowerShell references the real packed path" → Task 2 `resolveWindowsItemPath` wired into `BuildForWindowsRun` (commands) and `BuildForWindows` (service exec + overlay item). ✓
- "Linux/macOS byte-identical" → Task 2 `TestResolveItemsForPlatformLinuxNoExe` + windows gate. ✓
- "Idempotent / explicit-`.exe` safe" → Task 1 `TestApplyExeExtension` (`bin/myapp.exe` unchanged) and `TestResolveWindowsItemPath` (exact match); pre-existing `TestBuildForWindows`/`TestBuildForWindowsRunWithItems` use explicit `.exe` and stay green (Step 8). ✓
- Docs → Task 3. ✓

**Placeholder scan:** No TBD/TODO/“add error handling”/“similar to Task N”. Every code step contains the full code. ✓

**Type consistency:** `execTargetPaths(cfg *Config) map[string]bool`, `applyExeExtension(resolved []ResolvedItem, execTargets map[string]bool) []ResolvedItem`, `resolveWindowsItemPath(entries []FileEntry, ref string) string`, `ResolveItemsForPlatform(configDir string, cfg *Config, platform string) ([]FileEntry, error)` — signatures match across Task 1 (definition + tests), Task 2 (usage), and the Interfaces blocks. Field names `cfg.Commands`, `cfg.Services`, `CommandConfig.Exec`, `ServiceConfig.Exec`, `ResolvedItem.PackPath`, `FileEntry.RelPath` all match `config.go`/`packager.go`. ✓
