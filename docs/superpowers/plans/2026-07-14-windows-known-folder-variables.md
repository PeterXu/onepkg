# Windows Known-Folder Variables Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let config authors use a curated set of built-in Windows known-folder variables (e.g. `{AppDataFolder}`) inside path-bearing config fields, resolved on the Windows target at install/run time, in both install and run packages.

**Architecture:** A single source-of-truth map `installer.KnownFolders` (variable name → .NET `SpecialFolder` enum) lives in a new `installer/knownfolders.go`. It powers marker detection/validation, path-expression emission, and `$Var` definition emission. The generated PowerShell resolves each variable at runtime via `[Environment]::GetFolderPath([Environment+SpecialFolder]::<Enum>)`; Go never resolves a folder (the build host is macOS/Linux). Bare (unmarked) values emit byte-identical output to today.

**Tech Stack:** Go 1.21, module `onepkg`, TOML dep `github.com/pelletier/go-toml`. No new external dependencies.

## Global Constraints

- **Package layout:** `cmd/onepkg/*.go` is `package main`; `cmd/onepkg/installer/` is `package installer`.
- **TDD:** Write the failing test first, then implement. Every function gets a test.
- **Windows stubs are gitignored build artifacts** (`cmd/onepkg/installer/stubs/*.exe`) consumed via `//go:embed`. They are NOT modified by this feature. Run `make win-wrapper` (or `make build` / `make test`) once before building/testing, or the embed directives in `installer/exewrapper.go` fail.
- **Single source of truth:** valid variable names live in `installer.KnownFolders` (name → `SpecialFolder` enum). `config.go` (package `main`) imports `installer` to share it.
- **Resolution is install/run time only:** via `[Environment]::GetFolderPath([Environment+SpecialFolder]::<Enum>)` in the generated PowerShell; never in Go.
- **Syntax:** `{VarName}` in path-string fields (`dirs[].path`, `system_overlay[].system`, run `commands[].args`); `$VarName` in run `commands[].shell`.
- **Backward compatible:** unmarked values must emit byte-identical output to today (single-quoted literals via `psQuote`).
- **Windows-only:** marked `dirs`/`overlay` paths are rejected at build time for non-Windows platforms (run mode is already Windows-only).
- **Path validation:** reject `/..` and leading `..`; `..` within filenames is allowed (existing rule). The `..` check still applies to marked paths.

## File Structure

| File | Responsibility |
|------|----------------|
| `cmd/onepkg/installer/knownfolders.go` (new) | `KnownFolders` map (source of truth) + marker detection/validation, referenced-name collection (`{Name}` and `$Name`), `EmitPathExpr`, `EmitKnownFolderDefs`. |
| `cmd/onepkg/installer/knownfolders_test.go` (new) | Tests for every function above. |
| `cmd/onepkg/config.go` | Import `installer`; validate markers in `dirs[].path`, `system_overlay[].system`, run `args`; relax overlay absoluteness/critical-path checks for marked `system`. |
| `cmd/onepkg/config_test.go` | Validation acceptance/rejection tests + regression. |
| `cmd/onepkg/installer/powershell.go` | Emit `$<Name>` defs for names referenced by dirs/overlay; render marked `$DIR_<n>_PATH` / `$OVERLAY_<n>_SYSTEM` via `EmitPathExpr`. |
| `cmd/onepkg/installer/powershell_test.go` | Install emission tests + regression. |
| `cmd/onepkg/installer/powershell_runner.go` | Emit `$<Name>` defs for names referenced by `shell`/`args`; render marked args via `EmitPathExpr` (parenthesized for inline use). |
| `cmd/onepkg/installer/powershell_runner_test.go` | Run emission tests + regression. |
| `cmd/onepkg/main.go` | Build-time Windows-only guard for marked `dirs`/`overlay` paths in `BuildForPlatform`. |
| `cmd/onepkg/main_test.go` | Guard test (non-Windows error; Windows success) + regression. |
| `docs/config-example.toml` | Install + run examples using `{Var}` / `$Var`. |
| `CLAUDE.md` | Document the known-folder variable feature. |

---

## Task 1: Core known-folder primitives (`installer/knownfolders.go`)

**Files:**
- Create: `cmd/onepkg/installer/knownfolders.go`
- Test: `cmd/onepkg/installer/knownfolders_test.go`

**Interfaces:**
- Consumes: `psQuote(s string) string` (already in `installer/powershell.go`, same package).
- Produces (all in package `installer`, used by Tasks 2–5):
  - `KnownFolders map[string]string` — name → `SpecialFolder` enum.
  - `HasKnownFolderMarker(s string) bool`
  - `ValidateKnownFolderMarkers(s string) error`
  - `KnownFolderMarkers(s string) []string` — known names as `{Name}`, deduped+sorted.
  - `KnownFolderDollarVars(s string) []string` — known names as `$Name`, deduped+sorted.
  - `EmitKnownFolderDefs(names []string) string` — PS `$<Name> = ...` lines.
  - `EmitPathExpr(s string) string` — PS expression for a path-string value.

- [ ] **Step 1: Write the failing test**

Create `cmd/onepkg/installer/knownfolders_test.go`:

```go
package installer

import (
	"strings"
	"testing"
)

func TestKnownFoldersMap(t *testing.T) {
	if len(KnownFolders) == 0 {
		t.Fatal("KnownFolders map is empty")
	}
	cases := map[string]string{
		"AppDataFolder":        "ApplicationData",
		"LocalAppDataFolder":   "LocalApplicationData",
		"CommonAppDataFolder":  "CommonApplicationData",
		"ProgramFilesFolder":   "ProgramFilesX86",
		"ProgramFiles64Folder": "ProgramFiles",
		"WindowsFolder":        "Windows",
		"SystemFolder":         "System",
	}
	for name, enum := range cases {
		if got, ok := KnownFolders[name]; !ok {
			t.Errorf("KnownFolders missing %q", name)
		} else if got != enum {
			t.Errorf("KnownFolders[%q] = %q, want %q", name, got, enum)
		}
	}
}

func TestHasKnownFolderMarker(t *testing.T) {
	trues := []string{
		"{AppDataFolder}\\x",
		"{AppDataFolder}",
		"pre{LocalAppDataFolder}\\post",
	}
	for _, s := range trues {
		if !HasKnownFolderMarker(s) {
			t.Errorf("HasKnownFolderMarker(%q) = false, want true", s)
		}
	}
	falses := []string{
		"C:\\plain\\path",
		"",                  // empty
		"{AppData}",         // unknown name -> not a known marker
		"$AppDataFolder",    // dollar form is not a {marker}
	}
	for _, s := range falses {
		if HasKnownFolderMarker(s) {
			t.Errorf("HasKnownFolderMarker(%q) = true, want false", s)
		}
	}
}

func TestValidateKnownFolderMarkers(t *testing.T) {
	if err := ValidateKnownFolderMarkers("{AppDataFolder}\\MyApp"); err != nil {
		t.Errorf("known marker returned error: %v", err)
	}
	if err := ValidateKnownFolderMarkers("C:\\plain"); err != nil {
		t.Errorf("plain path returned error: %v", err)
	}
	if err := ValidateKnownFolderMarkers("{AppData}"); err == nil {
		t.Error("unknown {AppData} should return an error")
	}
	if err := ValidateKnownFolderMarkers("{AppDataFolder}{BogusFolder}"); err == nil {
		t.Error("mixed known+unknown should return an error naming BogusFolder")
	} else if !strings.Contains(err.Error(), "BogusFolder") {
		t.Errorf("error should name the unknown variable, got: %v", err)
	}
}

func TestKnownFolderMarkersAndDollarVars(t *testing.T) {
	got := KnownFolderMarkers("{LocalAppDataFolder}\\x{AppDataFolder}\\y{LocalAppDataFolder}{Unknown}")
	want := []string{"AppDataFolder", "LocalAppDataFolder"} // deduped + sorted, unknown excluded
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("KnownFolderMarkers = %v, want %v", got, want)
	}

	gotD := KnownFolderDollarVars("$AppDataFolder and $LocalAppDataFolder again $AppDataFolder and $Nope")
	wantD := []string{"AppDataFolder", "LocalAppDataFolder"}
	if len(gotD) != 2 || gotD[0] != wantD[0] || gotD[1] != wantD[1] {
		t.Errorf("KnownFolderDollarVars = %v, want %v", gotD, wantD)
	}
}

func TestEmitKnownFolderDefs(t *testing.T) {
	out := EmitKnownFolderDefs([]string{"LocalAppDataFolder", "AppDataFolder", "AppDataFolder"})
	if !strings.Contains(out, "$AppDataFolder = [Environment]::GetFolderPath([Environment+SpecialFolder]::ApplicationData)\n") {
		t.Errorf("missing/sorted AppDataFolder def, got:\n%s", out)
	}
	if !strings.Contains(out, "$LocalAppDataFolder = [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)\n") {
		t.Errorf("missing LocalAppDataFolder def, got:\n%s", out)
	}
	// AppDataFolder appears once (deduped).
	if strings.Count(out, "$AppDataFolder =") != 1 {
		t.Errorf("AppDataFolder def not deduped, got:\n%s", out)
	}
	if EmitKnownFolderDefs(nil) != "" {
		t.Error("EmitKnownFolderDefs(nil) should be empty")
	}
}

func TestEmitPathExpr(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\plain\path`, `'C:\plain\path'`},                                  // bare -> single-quoted literal
		{`{AppDataFolder}\MyApp\data`, `$AppDataFolder + '\MyApp\data'`},       // marker at start
		{`{AppDataFolder}`, `$AppDataFolder`},                                  // marker only
		{`pre\{LocalAppDataFolder}\post`, `'pre\' + $LocalAppDataFolder + '\post'`}, // marker in middle
		{`{AppDataFolder}\{LocalAppDataFolder}\x`, `$AppDataFolder + '\' + $LocalAppDataFolder + '\x'`}, // multiple
	}
	for _, c := range cases {
		got := EmitPathExpr(c.in)
		if got != c.want {
			t.Errorf("EmitPathExpr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test ./installer/ -run 'TestKnownFolders|TestHasKnownFolderMarker|TestValidateKnownFolder|TestKnownFolderMarkers|TestEmitKnownFolder|TestEmitPathExpr' -v`
Expected: FAIL / build error (`KnownFolders` undefined).

- [ ] **Step 3: Write minimal implementation**

Create `cmd/onepkg/installer/knownfolders.go`:

```go
package installer

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// KnownFolders maps a config variable name (the MSI/WiX folder property name)
// to its .NET SpecialFolder enum value, resolved on the Windows target at
// install/run time via [Environment]::GetFolderPath. This map is the single
// source of truth for valid variable names; config validation and the
// build-time guard both consult it.
var KnownFolders = map[string]string{
	"AppDataFolder":        "ApplicationData",
	"LocalAppDataFolder":   "LocalApplicationData",
	"CommonAppDataFolder":  "CommonApplicationData",
	"ProgramFilesFolder":   "ProgramFilesX86",
	"ProgramFiles64Folder": "ProgramFiles",
	"WindowsFolder":        "Windows",
	"SystemFolder":         "System",
	"DesktopFolder":        "DesktopDirectory",
	"ProgramMenuFolder":    "Programs",
	"StartupFolder":        "Startup",
	"PersonalFolder":       "MyDocuments",
	"CommonDocumentsFolder": "CommonDocuments",
}

// markerRe matches a {Name} token in path-string config fields.
var markerRe = regexp.MustCompile(`\{(\w+)\}`)

// dollarRe matches a $Name token as written in run-mode shell expressions.
var dollarRe = regexp.MustCompile(`\$([A-Za-z_]\w*)`)

// HasKnownFolderMarker reports whether s contains at least one {KnownName} token.
// Unknown braces such as {Foo} do not count; rejecting those is
// ValidateKnownFolderMarkers's job.
func HasKnownFolderMarker(s string) bool {
	return len(KnownFolderMarkers(s)) > 0
}

// ValidateKnownFolderMarkers returns an error if s contains any {X} token whose
// name X is not a known folder. Known markers are allowed.
func ValidateKnownFolderMarkers(s string) error {
	for _, m := range markerRe.FindAllStringSubmatch(s, -1) {
		if _, ok := KnownFolders[m[1]]; !ok {
			return fmt.Errorf("unknown Windows known-folder variable {%s}; supported: %s", m[1], knownFolderNameList())
		}
	}
	return nil
}

// KnownFolderMarkers returns the deduped, sorted known-folder names that appear
// as {Name} tokens in s.
func KnownFolderMarkers(s string) []string {
	var names []string
	for _, m := range markerRe.FindAllStringSubmatch(s, -1) {
		if _, ok := KnownFolders[m[1]]; ok {
			names = append(names, m[1])
		}
	}
	return dedupeSorted(names)
}

// KnownFolderDollarVars returns the deduped, sorted known-folder names that
// appear as $Name tokens in s (used to detect variables referenced in run shell).
func KnownFolderDollarVars(s string) []string {
	var names []string
	for _, m := range dollarRe.FindAllStringSubmatch(s, -1) {
		if _, ok := KnownFolders[m[1]]; ok {
			names = append(names, m[1])
		}
	}
	return dedupeSorted(names)
}

// EmitKnownFolderDefs emits a "$<Name> = [Environment]::GetFolderPath(...)" line
// for each name in names (deduped, sorted). Empty input produces empty output.
func EmitKnownFolderDefs(names []string) string {
	var b strings.Builder
	for _, n := range dedupeSorted(names) {
		fmt.Fprintf(&b, "$%s = [Environment]::GetFolderPath([Environment+SpecialFolder]::%s)\n", n, KnownFolders[n])
	}
	return b.String()
}

// EmitPathExpr renders a config path value as a PowerShell expression.
// A value without markers is a single-quoted literal (identical to psQuote).
// A value with {KnownName} markers is a concatenation referencing the
// runtime-resolved $<Name> variable, e.g. "$AppDataFolder + '\MyApp'".
// Unknown {X} tokens are left as single-quoted literals (safety; validation
// rejects them earlier).
func EmitPathExpr(s string) string {
	if !markerRe.MatchString(s) {
		return psQuote(s)
	}
	var parts []string
	idx := 0
	for _, m := range markerRe.FindAllStringSubmatchIndex(s, -1) {
		start, end := m[0], m[1]
		nameStart, nameEnd := m[2], m[3]
		if start > idx {
			parts = append(parts, psQuote(s[idx:start]))
		}
		name := s[nameStart:nameEnd]
		if _, ok := KnownFolders[name]; ok {
			parts = append(parts, "$"+name)
		} else {
			parts = append(parts, psQuote(s[start:end]))
		}
		idx = end
	}
	if idx < len(s) {
		parts = append(parts, psQuote(s[idx:]))
	}
	return strings.Join(parts, " + ")
}

func dedupeSorted(names []string) []string {
	seen := make(map[string]bool, len(names))
	var out []string
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func knownFolderNameList() string {
	names := make([]string, 0, len(KnownFolders))
	for n := range KnownFolders {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test ./installer/ -run 'TestKnownFolders|TestHasKnownFolderMarker|TestValidateKnownFolder|TestKnownFolderMarkers|TestEmitKnownFolder|TestEmitPathExpr' -v`
Expected: PASS (all listed tests).

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/installer/knownfolders.go cmd/onepkg/installer/knownfolders_test.go
git commit -m "feat(installer): add windows known-folder variable primitives"
```

---

## Task 2: Config validation of known-folder markers

**Files:**
- Modify: `cmd/onepkg/config.go` (import block; `validateInstall` dirs loop ~line 242, overlay loop ~line 263; `validateRun` command loop ~line 310)
- Test: `cmd/onepkg/config_test.go`

**Interfaces:**
- Consumes (from Task 1, package `installer`): `installer.HasKnownFolderMarker(s string) bool`, `installer.ValidateKnownFolderMarkers(s string) error`.
- Produces: no new exported symbols — extends existing `Validate` behavior.

- [ ] **Step 1: Write the failing test**

Append to `cmd/onepkg/config_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run 'TestValidateKnownFolder|TestValidateRejectsUnknownFolder|TestValidateOverlayUnmarked|TestValidateRunArgs' -v`
Expected: FAIL — `TestValidateKnownFolderInDirsAndOverlay` fails (overlay `system` `{ProgramFiles64Folder}\...` rejected as non-absolute); `TestValidateRejectsUnknownFolderMarker` may pass for the wrong reason or fail to compile once `installer` is referenced. (At minimum the accept-markers tests must fail.)

- [ ] **Step 3: Write minimal implementation**

In `cmd/onepkg/config.go`, add the import (the import block currently is `fmt`, `os`, `path/filepath`, `strings`, `toml`):

```go
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml"

	"onepkg/installer"
)
```

In `validateInstall`, in the `for _, d := range c.Dirs` loop, immediately after the `..` check, add marker validation:

```go
		if strings.Contains(d.Path, "..") {
			return fmt.Errorf("path traversal not allowed in dirs path %q", d.Path)
		}
		if err := installer.ValidateKnownFolderMarkers(d.Path); err != nil {
			return err
		}
```

In `validateInstall`, in the `for _, overlay := range c.SystemOverlay` loop, replace the current `overlay.System == "" || !filepath.IsAbs(...)` block with a marker-aware version. The current block is:

```go
		if overlay.System == "" || !filepath.IsAbs(overlay.System) {
			return fmt.Errorf("overlay system path %q must be an absolute path", overlay.System)
		}
		if strings.Contains(overlay.System, "..") {
			return fmt.Errorf("path traversal not allowed in overlay system path %q", overlay.System)
		}
		cleanSys := filepath.Clean(overlay.System)
		if isCriticalPath(cleanSys) {
			return fmt.Errorf("critical system path not allowed in overlay: %q", overlay.System)
		}
```

Replace it with:

```go
		if overlay.System == "" {
			return fmt.Errorf("overlay system path is required")
		}
		if err := installer.ValidateKnownFolderMarkers(overlay.System); err != nil {
			return err
		}
		if strings.Contains(overlay.System, "..") {
			return fmt.Errorf("path traversal not allowed in overlay system path %q", overlay.System)
		}
		// A marked system path (e.g. {AppDataFolder}\...) is absolute on the target
		// but cannot be Go-evaluated for absoluteness or critical-path safety, so
		// those checks only apply to literal paths.
		if !installer.HasKnownFolderMarker(overlay.System) {
			if !filepath.IsAbs(overlay.System) {
				return fmt.Errorf("overlay system path %q must be an absolute path", overlay.System)
			}
			cleanSys := filepath.Clean(overlay.System)
			if isCriticalPath(cleanSys) {
				return fmt.Errorf("critical system path not allowed in overlay: %q", overlay.System)
			}
		}
```

In `validateRun`, in the `for i, cmd := range c.Commands` loop, after the `if hasExec { ... }` block and before the loop closes, add args validation:

```go
		for _, a := range cmd.Args {
			if err := installer.ValidateKnownFolderMarkers(a); err != nil {
				return err
			}
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test -run 'TestValidate' -v`
Expected: PASS (the new tests and all pre-existing `TestValidate*` tests).

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/config.go cmd/onepkg/config_test.go
git commit -m "feat(config): validate windows known-folder markers in paths and run args"
```

---

## Task 3: Emit known-folder variables in `install.ps1`

**Files:**
- Modify: `cmd/onepkg/installer/powershell.go` (variables section ~line 105; dir path emission ~line 125; overlay system emission ~line 133)
- Test: `cmd/onepkg/installer/powershell_test.go`

**Interfaces:**
- Consumes (from Task 1, same package): `KnownFolderMarkers`, `EmitKnownFolderDefs`, `EmitPathExpr`.
- Produces: extended `GeneratePowerShellInstaller` output (no signature change).

- [ ] **Step 1: Write the failing test**

Append to `cmd/onepkg/installer/powershell_test.go`:

```go
func TestGeneratePowerShellInstallerKnownFolderDirs(t *testing.T) {
	cfg := PowerShellConfig{
		Name:         "myapp",
		Version:      "v1.0.0",
		Platform:     "windows-amd64",
		ExpectedArch: "AMD64",
		DefaultDir:   `C:\Program Files\myapp`,
		ServiceName:  "",
		Dirs: []DirSpec{
			{Path: `{AppDataFolder}\MyApp\data`, Owner: "myapp", Mode: "0755"},
		},
	}
	out, err := GeneratePowerShellInstaller(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "$AppDataFolder = [Environment]::GetFolderPath([Environment+SpecialFolder]::ApplicationData)") {
		t.Error("missing $AppDataFolder runtime definition")
	}
	want := `$DIR_0_PATH = $AppDataFolder + '\MyApp\data'`
	if !strings.Contains(out, want) {
		t.Errorf("missing marked dir path expression %q", want)
	}
}

func TestGeneratePowerShellInstallerKnownFolderOverlay(t *testing.T) {
	cfg := PowerShellConfig{
		Name:         "myapp",
		Version:      "v1.0.0",
		Platform:     "windows-amd64",
		ExpectedArch: "AMD64",
		DefaultDir:   `C:\Program Files\myapp`,
		ServiceName:  "",
		Overlays: []OverlaySpec{
			{System: `{ProgramFiles64Folder}\OtherApp\config.json`, Item: "etc/config.txt"},
		},
	}
	out, err := GeneratePowerShellInstaller(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "$ProgramFiles64Folder = [Environment]::GetFolderPath([Environment+SpecialFolder]::ProgramFiles)") {
		t.Error("missing $ProgramFiles64Folder runtime definition")
	}
	want := `$OVERLAY_0_SYSTEM = $ProgramFiles64Folder + '\OtherApp\config.json'`
	if !strings.Contains(out, want) {
		t.Errorf("missing marked overlay system expression %q", want)
	}
}

func TestGeneratePowerShellInstallerNoKnownFolderDefsWhenUnmarked(t *testing.T) {
	cfg := PowerShellConfig{
		Name:         "myapp",
		Version:      "v1.0.0",
		Platform:     "windows-amd64",
		ExpectedArch: "AMD64",
		DefaultDir:   `C:\Program Files\myapp`,
		ServiceName:  "",
		Dirs:         []DirSpec{{Path: `C:\var\log\myapp`, Mode: "0755"}},
		Overlays:     []OverlaySpec{{System: `C:\etc\agent\config.txt`, Item: "etc/config.txt"}},
	}
	out, err := GeneratePowerShellInstaller(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[Environment]::GetFolderPath") {
		t.Error("unmarked config should emit no known-folder definitions")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test ./installer/ -run 'TestGeneratePowerShellInstallerKnownFolder|TestGeneratePowerShellInstallerNoKnownFolderDefs' -v`
Expected: FAIL — defs absent; marked paths still rendered as literals.

- [ ] **Step 3: Write minimal implementation**

In `GeneratePowerShellInstaller` (`cmd/onepkg/installer/powershell.go`), immediately after the `$DEFAULT_DIR` line (`fmt.Fprintf(&b, "$DEFAULT_DIR = %s\n", psQuote(cfg.DefaultDir))`), collect referenced names and emit their definitions:

```go
	// Windows known-folder definitions: only the variables referenced by
	// dirs/overlay paths (bare configs emit nothing).
	var kfNames []string
	for _, d := range cfg.Dirs {
		kfNames = append(kfNames, KnownFolderMarkers(d.Path)...)
	}
	for _, o := range cfg.Overlays {
		kfNames = append(kfNames, KnownFolderMarkers(o.System)...)
	}
	b.WriteString(EmitKnownFolderDefs(kfNames))
```

Change the dir path emission from `psQuote(d.Path)` to `EmitPathExpr(d.Path)`:

```go
		fmt.Fprintf(&b, "$DIR_%d_PATH = %s\n", i, EmitPathExpr(d.Path))
```

Change the overlay system emission from `psQuote(o.System)` to `EmitPathExpr(o.System)`:

```go
		fmt.Fprintf(&b, "$OVERLAY_%d_SYSTEM = %s\n", i, EmitPathExpr(o.System))
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test ./installer/ -run 'TestGeneratePowerShellInstaller' -v`
Expected: PASS — new tests pass AND the pre-existing `TestGeneratePowerShellInstallerWithDirs` / `TestGeneratePowerShellInstallerWithOverlays` still pass (bare paths render byte-identically via `EmitPathExpr`).

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/installer/powershell.go cmd/onepkg/installer/powershell_test.go
git commit -m "feat(installer): resolve windows known-folder vars in install.ps1 paths"
```

---

## Task 4: Emit known-folder variables in `run.ps1`

**Files:**
- Modify: `cmd/onepkg/installer/powershell_runner.go` (after arch vars ~line 45; `runnerCommandBlock` arg rendering ~lines 84-86 and 91-93)
- Test: `cmd/onepkg/installer/powershell_runner_test.go`

**Interfaces:**
- Consumes (from Task 1, same package): `KnownFolderMarkers`, `KnownFolderDollarVars`, `EmitKnownFolderDefs`, `EmitPathExpr`.
- Produces: new unexported helper `argPS(a string) string`; extended `GeneratePowerShellRunner` output (no signature change).

- [ ] **Step 1: Write the failing test**

Append to `cmd/onepkg/installer/powershell_runner_test.go`:

```go
func TestGeneratePowerShellRunnerKnownFolders(t *testing.T) {
	cfg := PowerShellRunnerConfig{
		Name:         "kf",
		Version:      "v1.0.0",
		Platform:     "windows-amd64",
		ExpectedArch: "AMD64",
		Commands: []CommandSpec{
			{Exec: "bin/app.exe", Args: []string{"--config", `{LocalAppDataFolder}\MyApp\cfg.json`}},
			{Shell: `Copy-Item profile.json "$AppDataFolder\MyApp\"`},
		},
	}
	out, err := GeneratePowerShellRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "$AppDataFolder = [Environment]::GetFolderPath([Environment+SpecialFolder]::ApplicationData)") {
		t.Error("missing $AppDataFolder def (referenced via $Var in shell)")
	}
	if !strings.Contains(out, "$LocalAppDataFolder = [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)") {
		t.Error("missing $LocalAppDataFolder def (referenced via {Var} in args)")
	}
	// Marked arg rendered as a parenthesized concatenation in the invocation.
	if !strings.Contains(out, "($LocalAppDataFolder + '\\MyApp\\cfg.json')") {
		t.Errorf("missing marked arg expression in invocation; got:\n%s", out)
	}
}

func TestGeneratePowerShellRunnerNoKnownFolderDefsWhenUnused(t *testing.T) {
	cfg := PowerShellRunnerConfig{
		Name: "x", Version: "v1", Platform: "windows-amd64", ExpectedArch: "AMD64",
		Commands: []CommandSpec{{Shell: "Write-Host hi"}},
	}
	out, err := GeneratePowerShellRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[Environment]::GetFolderPath") {
		t.Error("runner with no variable references should emit no known-folder definitions")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test ./installer/ -run 'TestGeneratePowerShellRunnerKnownFolders|TestGeneratePowerShellRunnerNoKnownFolderDefsWhenUnused' -v`
Expected: FAIL — defs absent; marked arg still rendered as a literal.

- [ ] **Step 3: Write minimal implementation**

In `GeneratePowerShellRunner` (`cmd/onepkg/installer/powershell_runner.go`), after the arch-vars line (`b.WriteString(powershellArchVars(cfg.ExpectedArch))`) and its following `b.WriteString("\n")`, collect referenced names from `shell` (`$Name`) and `args` (`{Name}`) and emit definitions:

```go
	// Windows known-folder definitions: only variables referenced by the
	// commands (via $Name in shell or {Name} in args).
	var kfNames []string
	for _, cmd := range cfg.Commands {
		if cmd.Shell != "" {
			kfNames = append(kfNames, KnownFolderDollarVars(cmd.Shell)...)
		}
		for _, a := range cmd.Args {
			kfNames = append(kfNames, KnownFolderMarkers(a)...)
		}
	}
	b.WriteString(EmitKnownFolderDefs(kfNames))
```

Add the `argPS` helper (place it near `runnerCommandBlock`):

```go
// argPS renders a command argument for inline PowerShell use. Bare values are
// single-quoted (identical to psQuote); marked values become a parenthesized
// concatenation so they are a single token in the log line and the invocation.
func argPS(a string) string {
	e := EmitPathExpr(a)
	if strings.Contains(e, " + ") {
		return "(" + e + ")"
	}
	return e
}
```

In `runnerCommandBlock`, in the `cmd.Exec != ""` branch, replace both argument-rendering loops (the `Write-Host 'Running:'` log line and the `& (Join-Path $PayloadDir ...)` invocation) so they use `argPS(a)` instead of `psQuote(a)`:

```go
		b.WriteString("    Write-Host 'Running:'")
		b.WriteString(" " + psQuote(cmd.Exec))
		for _, a := range cmd.Args {
			b.WriteString(" " + argPS(a))
		}
		b.WriteString("\n")

		b.WriteString("    $LASTEXITCODE = 0\n")
		b.WriteString("    & (Join-Path $PayloadDir " + psQuote(cmd.Exec) + ")")
		for _, a := range cmd.Args {
			b.WriteString(" " + argPS(a))
		}
		b.WriteString("\n")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && go test ./installer/ -run 'TestGeneratePowerShellRunner' -v`
Expected: PASS — new tests pass AND pre-existing `TestGeneratePowerShellRunner` still passes (bare args `'--all'`, `'--force'` render identically).

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/installer/powershell_runner.go cmd/onepkg/installer/powershell_runner_test.go
git commit -m "feat(installer): resolve windows known-folder vars in run.ps1 args/shell"
```

---

## Task 5: Build-time Windows-only guard

**Files:**
- Modify: `cmd/onepkg/main.go` (`BuildForPlatform`, immediately after the run-mode guard ~line 200-202)
- Test: `cmd/onepkg/main_test.go`

**Interfaces:**
- Consumes (from Task 1, package `installer`): `installer.HasKnownFolderMarker(s string) bool`.
- Produces: no new symbols — extended `BuildForPlatform` behavior.

- [ ] **Step 1: Write the failing test**

Append to `cmd/onepkg/main_test.go`:

```go
func TestBuildForPlatformRejectsKnownFolderOnLinux(t *testing.T) {
	cfg := &Config{
		Name:    "kf",
		Version: "v1",
		Mode:    "install",
		Dirs:    []DirConfig{{Path: `{AppDataFolder}\MyApp`, Mode: "0755"}},
	}
	err := BuildForPlatform(cfg, "linux-amd64", nil, nil, nil, "")
	if err == nil {
		t.Fatal("BuildForPlatform should reject a marked dirs path on linux")
	}
	if !strings.Contains(err.Error(), "not supported on linux") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildForPlatformRejectsKnownFolderOverlayOnDarwin(t *testing.T) {
	cfg := &Config{
		Name:          "kf",
		Version:       "v1",
		Mode:          "install",
		SystemOverlay: []OverlayConfig{{System: `{ProgramFiles64Folder}\OtherApp\cfg`, Item: "etc/cfg"}},
	}
	err := BuildForPlatform(cfg, "darwin-arm64", nil, nil, nil, "")
	if err == nil {
		t.Fatal("BuildForPlatform should reject a marked overlay system on darwin")
	}
	if !strings.Contains(err.Error(), "not supported on darwin") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildForPlatformWindowsAllowsKnownFolderDirs(t *testing.T) {
	projectDir := t.TempDir()
	binDir := filepath.Join(projectDir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "myapp.exe"), []byte("fake-exe"), 0755)

	content := `
name = "myapp"
version = "v1.0.0"
items = ["bin/myapp.exe"]

[[dirs]]
path = "{AppDataFolder}\\MyApp\\data"
mode = "0755"
`
	configPath := filepath.Join(projectDir, "config.toml")
	os.WriteFile(configPath, []byte(content), 0644)

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	resolved, err := ResolveItems(projectDir, cfg.Items, "windows-amd64")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := CollectFilesResolved(projectDir, resolved, cfg.Exclude)
	if err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(projectDir, "packages")
	os.MkdirAll(outputDir, 0755)

	if err := BuildForPlatform(cfg, "windows-amd64", entries, nil, nil, outputDir); err != nil {
		t.Fatalf("BuildForPlatform on windows should accept marked dirs: %v", err)
	}
	exe, err := os.ReadFile(filepath.Join(outputDir, "myapp_v1.0.0_windows_amd64.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(exe, []byte("$AppDataFolder = [Environment]::GetFolderPath([Environment+SpecialFolder]::ApplicationData)")) {
		t.Error("embedded install.ps1 missing the $AppDataFolder definition")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd cmd/onepkg && go test -run 'TestBuildForPlatformRejectsKnownFolder|TestBuildForPlatformWindowsAllowsKnownFolderDirs' -v`
Expected: FAIL — the linux/darwin builds do not error today; the windows test passes already (it asserts only pre-existing behavior + Task 3 output, which is now in place). The two `Rejects*` tests fail.

- [ ] **Step 3: Write minimal implementation**

In `BuildForPlatform` (`cmd/onepkg/main.go`), immediately after the existing run-mode guard:

```go
	if cfg.Mode == "run" && goos != "windows" {
		return fmt.Errorf("mode=\"run\" is Windows-only; got platform %q", platform)
	}

	// Windows known-folder variables in install path fields are not resolvable on
	// Linux/macOS targets; fail fast rather than emit a meaningless literal.
	if goos != "windows" {
		for _, d := range cfg.Dirs {
			if installer.HasKnownFolderMarker(d.Path) {
				return fmt.Errorf("Windows known-folder variable in dirs[].path %q is not supported on %s; build for windows or remove it", d.Path, goos)
			}
		}
		for _, o := range cfg.SystemOverlay {
			if installer.HasKnownFolderMarker(o.System) {
				return fmt.Errorf("Windows known-folder variable in system_overlay[].system %q is not supported on %s; build for windows or remove it", o.System, goos)
			}
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd cmd/onepkg && make build && go test ./... `
Expected: PASS (build regenerates stubs; all packages green). Specifically the three new tests pass and existing tests still pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/main.go cmd/onepkg/main_test.go
git commit -m "feat(build): reject windows known-folder vars on non-windows platforms"
```

---

## Task 6: Documentation

**Files:**
- Modify: `docs/config-example.toml` (append a known-folder section; note Windows-only nature)
- Modify: `CLAUDE.md` (add a Key Conventions bullet)

**Interfaces:** None (docs only).

- [ ] **Step 1: Append the known-folder section to `docs/config-example.toml`**

Append at the end of the file:

```toml

# --- Windows known-folder variables (Windows-only) ---
# A curated set of built-in Windows known folders can be referenced in path
# fields and resolved on the TARGET machine at install/run time via
# [Environment]::GetFolderPath. They are NOT resolved at build time (the build
# host is macOS/Linux), so the target user's profile is used.
#
# Syntax:
#   {VarName}  in path-string fields: dirs[].path, system_overlay[].system,
#              and run-mode commands[].args
#   $VarName   in run-mode commands[].shell (ordinary PowerShell variable)
#
# Supported variables (MSI/WiX names):
#   AppDataFolder         C:\Users\<u>\AppData\Roaming
#   LocalAppDataFolder    C:\Users\<u>\AppData\Local
#   CommonAppDataFolder   C:\ProgramData
#   ProgramFilesFolder    C:\Program Files (x86)
#   ProgramFiles64Folder  C:\Program Files
#   WindowsFolder         C:\Windows
#   SystemFolder          C:\Windows\System32
#   DesktopFolder         C:\Users\<u>\Desktop
#   ProgramMenuFolder     Start Menu\Programs
#   StartupFolder         Start Menu\Programs\Startup
#   PersonalFolder        C:\Users\<u>\Documents
#   CommonDocumentsFolder C:\Users\Public\Documents
#
# Unknown names (e.g. {AppData}) fail fast at config validation.
# Marked dirs/overlay paths are Windows-only: building them for linux/darwin is
# an error. (Run mode is already Windows-only.)
#
# Install example - create a directory under the roaming AppData folder:
# [[dirs]]
# path = "{AppDataFolder}\\MyApp\\data"
# mode = "0755"
#
# Install example - overlay a file from another installed app:
# [[system_overlay]]
# system = "{ProgramFiles64Folder}\\OtherApp\\config.json"
# item = "config/otherapp.json"
#
# Run example - pass a resolved path as an argument, and use $Var in shell:
# [[commands]]
# exec = "bin/myapp.exe"
# args = ["--config", "{AppDataFolder}\\MyApp\\config.json"]
#
# [[commands]]
# shell = "Copy-Item profile.json \"$AppDataFolder\\MyApp\\\""
```

- [ ] **Step 2: Add a Key Conventions bullet to `CLAUDE.md`**

In the `## Key Conventions` section, add (e.g. after the `mode = "run"` bullet):

```markdown
- **Windows known-folder variables:** `{VarName}` (e.g. `{AppDataFolder}`) in `dirs[].path`, `system_overlay[].system`, and run-mode `commands[].args`; `$VarName` in run-mode `commands[].shell`. Resolved at install/run time in the generated PowerShell via `[Environment]::GetFolderPath`; never in Go. Windows-only (marked `dirs`/`overlay` are rejected on non-Windows builds). Curated set in `installer.KnownFolders`. Design spec: `docs/superpowers/specs/2026-07-14-windows-known-folder-variables-design.md`.
```

- [ ] **Step 3: Verify the example still parses and the suite is green**

Run: `cd cmd/onepkg && make test`
Expected: PASS — all packages green. (The example file is documentation and is not parsed by tests, but `make test` confirms nothing regressed.)

- [ ] **Step 4: Commit**

```bash
git add docs/config-example.toml CLAUDE.md
git commit -m "docs: document windows known-folder variables"
```

---

## Self-Review Notes

**Spec coverage:**
- Variable set + map (single source of truth) → Task 1.
- `{Var}` in `dirs[].path` / `system_overlay[].system` → Tasks 2 (validate) + 3 (emit).
- `$Var` in run `shell` + `{Var}` in run `args` → Tasks 2 (validate args) + 4 (emit).
- Unknown-marker fail-fast → Task 2 (`ValidateKnownFolderMarkers`).
- Overlay absoluteness/critical-path relaxation for marked `system` → Task 2.
- Windows-only build guard → Task 5.
- Backward compatibility (bare → byte-identical) → `EmitPathExpr` returns `psQuote(s)` when unmarked (Task 1); regression assertions in Tasks 3 & 4.
- Docs → Task 6.

**Type consistency:** `argPS` (Task 4) wraps `EmitPathExpr` (Task 1); both Tasks 3 & 4 call `EmitPathExpr`/`EmitKnownFolderDefs`/`KnownFolderMarkers` exactly as defined in Task 1; Task 2 & 5 call `installer.HasKnownFolderMarker`/`installer.ValidateKnownFolderMarkers` exactly as defined in Task 1. No name drift.

**Open / deferred (call out in the final review):** actual `[Environment]::GetFolderPath` runtime values cannot be exercised on macOS (same gap as existing Windows support).
