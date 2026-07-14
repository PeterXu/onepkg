# Config Marker Namespaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unify config variable markers under namespaces — add build-time `{env.NAME}` host-variable substitution, rename the known-folder markers from `{NAME}` to `{dst.NAME}`, and fail fast on `{dst.NAME}` used outside the fields that resolve it.

**Architecture:** Three mutually-exclusive `{...}` markers by prefix: `{platform}` (legacy bare, build-time Go), `{env.NAME}` (host env, build-time Go via a one-shot reflection walk), `{dst.NAME}` (Windows known folders, resolved on the target in PowerShell). The `{dst.NAME}` change is a surface rename of the already-merged known-folder mechanism (the resolution codegen is unchanged); the new `{env.NAME}` substitution is a pre-validation pass in package `main` shared with a new stray-marker validator via one reflection walker.

**Tech Stack:** Go 1.21, module `onepkg`, TOML dep `github.com/pelletier/go-toml`. No new dependencies.

## Global Constraints

- Go 1.21, module name `onepkg`; TOML dependency is `github.com/pelletier/go-toml` (NOT BurntSushi despite CLAUDE.md).
- Do NOT modify `build-package/` (symlink to another project).
- Windows stubs are gitignored build artifacts. `//go:embed` in `installer/exewrapper.go` fails if `cmd/onepkg/installer/stubs/*.exe` are missing — run `make win-wrapper` (or `make build`) once before any `go test`. `make test` regenerates stubs then runs the suites.
- Generated shell scripts use `set -euo pipefail`.
- The three markers and their regexes are mutually exclusive by prefix: `{platform}` (literal substring), `{env.NAME}` (`\{env\.([A-Za-z_][A-Za-z0-9_]*)\}`), `{dst.NAME}` (`\{dst\.(\w+)\}`).
- `{env.NAME}` resolves at BUILD time from the build host via `os.Getenv`; unset OR empty → build-failing error listing all offenders (deduped, sorted). No inline defaults.
- `{dst.NAME}` resolves on the TARGET at install/run time in generated PowerShell via `[Environment]::GetFolderPath`; never in Go (build host is macOS/Linux). Resolution mechanism is unchanged from the merged feature — only the marker surface is renamed `{NAME}` → `{dst.NAME}`.
- The `{NAME}` → `{dst.NAME}` rename is intentionally breaking but local-only (the known-folder feature is not pushed; tests + `docs/config-example.toml` are migrated in this same change).
- `{dst.NAME}` is only valid in `dirs[].path`, `system_overlay[].system`, and run-mode `commands[].args` / `commands[].shell`; anywhere else is a config error.
- Single service only (first `[[services]]` entry).
- TDD throughout: failing test first, then implementation.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `cmd/onepkg/installer/knownfolders.go` | Single source of truth for known folders + the `{dst.NAME}` marker helpers (`HasKnownFolderMarker`, `ValidateKnownFolderMarkers`, `KnownFolderMarkers`, `EmitKnownFolderDefs`, `EmitPathExpr`, new `ReplaceMarkers`). Marker regex changes `\{(\w+)\}` → `\{dst\.(\w+)\}`; the run-shell `$Name` detection (`dollarRe`/`KnownFolderDollarVars`) is removed in favor of uniform `{dst.NAME}`. |
| `cmd/onepkg/installer/powershell_runner.go` | Run-mode `run.ps1` generation. Shell now collects `{dst.NAME}` via `KnownFolderMarkers` and emits the shell with `{dst.NAME}` → `$Name` via `ReplaceMarkers`. |
| `cmd/onepkg/markers.go` (NEW, package main) | The shared reflection walker `walkStrings`, `{env.NAME}` substitution (`ExpandEnv`, `expandEnvString`, `envMarkerRe`), and stray-`{dst.NAME}` validation (`validateNoStrayDstMarkers`, `isDstSupportedField`, `dstMarkerRe`). |
| `cmd/onepkg/config.go` | Calls `validateNoStrayDstMarkers()` from `Validate`. No other code change (the `installer.ValidateKnownFolderMarkers`/`HasKnownFolderMarker` call sites keep their signatures; the marker form is internal to `installer`). One comment updated. |
| `cmd/onepkg/main.go` | New `loadConfig(path) (*Config, error)` seam = ParseConfig → `ExpandEnv` → ApplyDefaults, so the `{env.NAME}` wiring is unit-testable. `main()` calls it before `Validate`. |
| Tests (`*_test.go`) | TDD coverage per task. |

---

### Task 1: Rename `{NAME}` → `{dst.NAME}` across the installer package and tests (atomic)

This is one atomic rename: removing `KnownFolderDollarVars` breaks `powershell_runner.go`, and renaming the marker regex breaks every test that authors a bare `{Name}`, so the source and all tests must land together. The suite is green only at the final step.

**Files:**
- Modify: `cmd/onepkg/installer/knownfolders.go`
- Modify: `cmd/onepkg/installer/powershell_runner.go`
- Modify: `cmd/onepkg/config.go:281` (comment only)
- Modify: `cmd/onepkg/installer/knownfolders_test.go`
- Modify: `cmd/onepkg/installer/powershell_test.go` (lines ~195, ~220)
- Modify: `cmd/onepkg/installer/powershell_runner_test.go` (lines ~101-102, ~109-117)
- Modify: `cmd/onepkg/config_test.go` (lines ~1740, ~1744, ~1772, ~1828)
- Modify: `cmd/onepkg/main_test.go` (lines ~596, ~612, ~635)

**Interfaces:**
- Produces (changed marker form, unchanged signatures): `installer.HasKnownFolderMarker(string) bool`, `installer.ValidateKnownFolderMarkers(string) error`, `installer.KnownFolderMarkers(string) []string`, `installer.EmitKnownFolderDefs([]string) string`, `installer.EmitPathExpr(string) string` — all now operate on `{dst.NAME}`. NEW: `installer.ReplaceMarkers(string) string` (replaces `{dst.NAME}` → `$Name`). REMOVED: `installer.KnownFolderDollarVars`, `installer.dollarRe`.

- [ ] **Step 1: Update `knownfolders_test.go` to expect `{dst.NAME}`**

Replace the entire file body with:

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
		"{dst.AppDataFolder}\\x",
		"{dst.AppDataFolder}",
		"pre{dst.LocalAppDataFolder}\\post",
	}
	for _, s := range trues {
		if !HasKnownFolderMarker(s) {
			t.Errorf("HasKnownFolderMarker(%q) = false, want true", s)
		}
	}
	falses := []string{
		"C:\\plain\\path",
		"",                  // empty
		"{AppDataFolder}",   // bare form: no dst. prefix -> not matched
		"{dst.AppData}",     // dst. prefix but unknown name -> not a known marker
		"$AppDataFolder",    // dollar form is not a {dst.} marker
	}
	for _, s := range falses {
		if HasKnownFolderMarker(s) {
			t.Errorf("HasKnownFolderMarker(%q) = true, want false", s)
		}
	}
}

func TestValidateKnownFolderMarkers(t *testing.T) {
	if err := ValidateKnownFolderMarkers("{dst.AppDataFolder}\\MyApp"); err != nil {
		t.Errorf("known marker returned error: %v", err)
	}
	if err := ValidateKnownFolderMarkers("C:\\plain"); err != nil {
		t.Errorf("plain path returned error: %v", err)
	}
	if err := ValidateKnownFolderMarkers("{dst.AppData}"); err == nil {
		t.Error("unknown {dst.AppData} should return an error")
	} else if !strings.Contains(err.Error(), "AppData") {
		t.Errorf("error should name the unknown variable, got: %v", err)
	}
	if err := ValidateKnownFolderMarkers("{dst.AppDataFolder}{dst.BogusFolder}"); err == nil {
		t.Error("mixed known+unknown should return an error naming BogusFolder")
	} else if !strings.Contains(err.Error(), "BogusFolder") {
		t.Errorf("error should name the unknown variable, got: %v", err)
	}
}

func TestKnownFolderMarkersAndReplaceMarkers(t *testing.T) {
	got := KnownFolderMarkers("{dst.LocalAppDataFolder}\\x{dst.AppDataFolder}\\y{dst.LocalAppDataFolder}{dst.Unknown}")
	want := []string{"AppDataFolder", "LocalAppDataFolder"} // deduped + sorted, unknown excluded
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("KnownFolderMarkers = %v, want %v", got, want)
	}

	gotR := ReplaceMarkers(`pre{dst.AppDataFolder}\x{dst.LocalAppDataFolder}\y{dst.Unknown}`)
	wantR := `pre$AppDataFolder\x$LocalAppDataFolder\y{dst.Unknown}` // known -> $Name; unknown left intact
	if gotR != wantR {
		t.Errorf("ReplaceMarkers = %q, want %q", gotR, wantR)
	}
	if ReplaceMarkers("no markers here") != "no markers here" {
		t.Error("ReplaceMarkers should be a no-op on marker-free strings")
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
	if strings.Count(out, "$AppDataFolder =") != 1 {
		t.Errorf("AppDataFolder def not deduped, got:\n%s", out)
	}
	if EmitKnownFolderDefs(nil) != "" {
		t.Error("EmitKnownFolderDefs(nil) should be empty")
	}
}

func TestEmitPathExpr(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\plain\path`, `'C:\plain\path'`},                                                              // bare -> single-quoted literal
		{`{dst.AppDataFolder}\MyApp\data`, `$AppDataFolder + '\MyApp\data'`},                              // marker at start
		{`{dst.AppDataFolder}`, `$AppDataFolder`},                                                         // marker only
		{`pre\{dst.LocalAppDataFolder}\post`, `'pre\' + $LocalAppDataFolder + '\post'`},                   // marker in middle
		{`{dst.AppDataFolder}\{dst.LocalAppDataFolder}\x`, `$AppDataFolder + '\' + $LocalAppDataFolder + '\x'`}, // multiple
	}
	for _, c := range cases {
		got := EmitPathExpr(c.in)
		if got != c.want {
			t.Errorf("EmitPathExpr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Update `knownfolders.go` — regex, remove dollar-form, add `ReplaceMarkers`**

Make four edits to `cmd/onepkg/installer/knownfolders.go`:

Edit A — replace the two regex declarations (lines 30-34):

```go
// markerRe matches a {dst.Name} token in path-string config fields. The captured
// group is the bare folder name (e.g. "AppDataFolder"), which keys KnownFolders.
var markerRe = regexp.MustCompile(`\{dst\.(\w+)\}`)
```

(Remove the `dollarRe` declaration entirely.)

Edit B — replace `ValidateKnownFolderMarkers`'s error string (so it reports the `{dst.}` form):

```go
// ValidateKnownFolderMarkers returns an error if s contains any {dst.X} token whose
// name X is not a known folder. Known markers are allowed.
func ValidateKnownFolderMarkers(s string) error {
	for _, m := range markerRe.FindAllStringSubmatch(s, -1) {
		if _, ok := KnownFolders[m[1]]; !ok {
			return fmt.Errorf("unknown Windows known-folder variable {dst.%s}; supported: %s", m[1], knownFolderNameList())
		}
	}
	return nil
}
```

Edit C — replace `KnownFolderDollarVars` (lines 66-76) with `ReplaceMarkers`:

```go
// ReplaceMarkers replaces each known {dst.Name} token in s with the PowerShell
// variable reference $Name, for inline use inside run-mode shell expressions.
// Unknown {dst.X} tokens are left in place; validation rejects them earlier.
func ReplaceMarkers(s string) string {
	return markerRe.ReplaceAllStringFunc(s, func(tok string) string {
		m := markerRe.FindStringSubmatch(tok)
		if _, ok := KnownFolders[m[1]]; ok {
			return "$" + m[1]
		}
		return tok
	})
}
```

Edit D — update the `EmitPathExpr` doc comment's marker reference (lines 88-93) from `{KnownName}` to `{dst.Name}`:

```go
// EmitPathExpr renders a config path value as a PowerShell expression.
// A value without markers is a single-quoted literal (identical to psQuote).
// A value with {dst.Name} markers is a concatenation referencing the
// runtime-resolved $<Name> variable, e.g. "$AppDataFolder + '\MyApp'".
// Unknown {dst.X} tokens are left as single-quoted literals (safety; validation
// rejects them earlier).
```

- [ ] **Step 3: Update `powershell_runner.go` — shell collects `{dst.NAME}`, emits via `ReplaceMarkers`**

Edit A — the known-folder collection comment + shell branch (lines 48-59). Replace:

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
```

with:

```go
	// Windows known-folder definitions: only variables referenced by the commands
	// (via {dst.Name} in shell or args).
	var kfNames []string
	for _, cmd := range cfg.Commands {
		if cmd.Shell != "" {
			kfNames = append(kfNames, KnownFolderMarkers(cmd.Shell)...)
		}
		for _, a := range cmd.Args {
			kfNames = append(kfNames, KnownFolderMarkers(a)...)
		}
	}
```

Edit B — the shell command block (lines 120-128). Replace the two `psQuote(cmd.Shell)` uses with `psQuote(ReplaceMarkers(cmd.Shell))`:

```go
	} else {
		b.WriteString("    Write-Host 'Running:' " + psQuote(ReplaceMarkers(cmd.Shell)) + "\n")
		// Reset $LASTEXITCODE so we can detect failures. $? reflects the evaluated expression's success,
		// but for non-terminating errors inside Invoke-Expression, may not reliably become $false depending on
		// $ErrorActionPreference. $LASTEXITCODE is the reliable signal for native-exe failures; $? catches terminating/cmdlet failures.
		b.WriteString("    $LASTEXITCODE = 0\n")
		b.WriteString("    Invoke-Expression " + psQuote(ReplaceMarkers(cmd.Shell)) + "\n")
		b.WriteString("    if ((-not $?) -or ($LASTEXITCODE -ne 0)) { Write-Host 'Command failed'; Read-Host 'Press Enter to exit'; exit 1 }\n")
	}
```

- [ ] **Step 4: Update installer install/run test inputs + expectations to `{dst.NAME}`**

In `cmd/onepkg/installer/powershell_test.go`:
- `TestGeneratePowerShellInstallerKnownFolderDirs` (line 195): change the DirSpec path to `` `{dst.AppDataFolder}\MyApp\data` ``. Output expectations (lines 202-205) are unchanged (`$AppDataFolder` def + `$DIR_0_PATH = $AppDataFolder + '\MyApp\data'`).
- `TestGeneratePowerShellInstallerKnownFolderOverlay` (line 220): change the OverlaySpec system to `` `{dst.ProgramFiles64Folder}\OtherApp\config.json` ``. Output expectations (lines 227-230) unchanged.

In `cmd/onepkg/installer/powershell_runner_test.go`, replace the body of `TestGeneratePowerShellRunnerKnownFolders` (lines 94-119) with:

```go
func TestGeneratePowerShellRunnerKnownFolders(t *testing.T) {
	cfg := PowerShellRunnerConfig{
		Name:         "kf",
		Version:      "v1.0.0",
		Platform:     "windows-amd64",
		ExpectedArch: "AMD64",
		Commands: []CommandSpec{
			{Exec: "bin/app.exe", Args: []string{"--config", `{dst.LocalAppDataFolder}\MyApp\cfg.json`}},
			{Shell: `Copy-Item profile.json "{dst.AppDataFolder}\MyApp\"`},
		},
	}
	out, err := GeneratePowerShellRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "$AppDataFolder = [Environment]::GetFolderPath([Environment+SpecialFolder]::ApplicationData)") {
		t.Error("missing $AppDataFolder def (referenced via {dst.Name} in shell)")
	}
	if !strings.Contains(out, "$LocalAppDataFolder = [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)") {
		t.Error("missing $LocalAppDataFolder def (referenced via {dst.Name} in args)")
	}
	// Marked arg rendered as a parenthesized concatenation in the invocation.
	if !strings.Contains(out, "($LocalAppDataFolder + '\\MyApp\\cfg.json')") {
		t.Errorf("missing marked arg expression in invocation; got:\n%s", out)
	}
	// Shell's {dst.AppDataFolder} is rewritten to $AppDataFolder in the emitted Invoke-Expression.
	wantShell := `Invoke-Expression 'Copy-Item profile.json "$AppDataFolder\MyApp\"'`
	if !strings.Contains(out, wantShell) {
		t.Errorf("shell marker not rewritten to $Var; want %q, got:\n%s", wantShell, out)
	}
}
```

- [ ] **Step 5: Update `config_test.go`, `main_test.go`, and the `config.go` comment**

In `cmd/onepkg/config_test.go`:
- Line 1740: `path = "{AppDataFolder}\\MyApp\\data"` → `path = "{dst.AppDataFolder}\\MyApp\\data"`.
- Line 1744: `system = "{ProgramFiles64Folder}\\OtherApp\\config.json"` → `system = "{dst.ProgramFiles64Folder}\\OtherApp\\config.json"`.
- Line 1772 (the unknown-marker test): `path = "{AppData}\\MyApp"` → `path = "{dst.AppData}\\MyApp"`. (The bare `{AppData}` no longer matches the `{dst.}` regex, so it would not error; the dst-form unknown `{dst.AppData}` does.)
- Line 1828: `args = ["--config", "{AppDataFolder}\\MyApp\\config.json"]` → `args = ["--config", "{dst.AppDataFolder}\\MyApp\\config.json"]`.

In `cmd/onepkg/main_test.go`:
- Line 596: `` {Path: `{AppDataFolder}\MyApp`, ...} `` → `` {Path: `{dst.AppDataFolder}\MyApp`, ...} ``.
- Line 612: `` {System: `{ProgramFiles64Folder}\OtherApp\cfg`, ...} `` → `` {System: `{dst.ProgramFiles64Folder}\OtherApp\cfg`, ...} ``.
- Line 635: `path = "{AppDataFolder}\\MyApp\\data"` → `path = "{dst.AppDataFolder}\\MyApp\\data"`.
- Output expectations at lines 667-668 are unchanged (`$AppDataFolder` def).

In `cmd/onepkg/config.go` line 281, update the comment so the example marker matches the new form:

```go
		// A marked system path (e.g. {dst.AppDataFolder}\...) is absolute on the target
```

- [ ] **Step 6: Run the full suite**

Run: `make test`
Expected: GREEN — `onepkg`, `onepkg/installer` suites pass; all known-folder marker inputs now use `{dst.NAME}`, run-shell emits `$Var`, and the build guard still rejects marked paths on linux/darwin.

If stubs are missing the build fails on `//go:embed`; `make test` regenerates them first.

- [ ] **Step 7: Commit**

```bash
git add cmd/onepkg/installer/knownfolders.go cmd/onepkg/installer/powershell_runner.go \
        cmd/onepkg/installer/knownfolders_test.go cmd/onepkg/installer/powershell_test.go \
        cmd/onepkg/installer/powershell_runner_test.go cmd/onepkg/config.go \
        cmd/onepkg/config_test.go cmd/onepkg/main_test.go
git commit -m "refactor(markers): rename known-folder markers to {dst.NAME}, unify run shell

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: `{env.NAME}` host-variable substitution + `loadConfig` seam

Add the build-time `{env.NAME}` substitution as a reflection walk over every config string field, plus a `loadConfig` helper in `main.go` so the Parse→ExpandEnv→ApplyDefaults wiring is unit-testable.

**Files:**
- Create: `cmd/onepkg/markers.go`
- Modify: `cmd/onepkg/main.go` (new `loadConfig`; `main()` uses it)
- Modify: `cmd/onepkg/config_test.go` (add tests) — note: add `TestExpandEnv*` and `TestLoadConfig*` tests

**Interfaces:**
- Produces: `(*Config) ExpandEnv() error` — substitutes `{env.NAME}` in place across the whole config; returns an error listing unset/empty offenders. Internal helpers: `walkStrings(reflect.Value, func(path string, s *string))` (shared walker, also consumed by Task 3), `expandEnvString(string) (string, []string)`, `envMarkerRe`. Also `loadConfig(configPath string) (*Config, error)` in `main.go`.

- [ ] **Step 1: Write the failing tests in `config_test.go`**

Append to `cmd/onepkg/config_test.go`:

```go
func TestExpandEnvSubstitutesAllStringFields(t *testing.T) {
	t.Setenv("ONEPKG_T2_NAME", "fromenv")
	t.Setenv("ONEPKG_T2_ROOT", "/opt/root")
	cfg := &Config{
		Name:  "{env.ONEPKG_T2_NAME}",
		Items: []string{"{env.ONEPKG_T2_ROOT}/bin/app", "etc/cfg"},
		Dirs:  []DirConfig{{Path: `{env.ONEPKG_T2_ROOT}\{dst.AppDataFolder}\x`}},
		Services: []ServiceConfig{{
			Linux: LinuxService{Environment: map[string]string{"TOKEN": "{env.ONEPKG_T2_NAME}"}},
		}},
	}
	if err := cfg.ExpandEnv(); err != nil {
		t.Fatalf("ExpandEnv: %v", err)
	}
	if cfg.Name != "fromenv" {
		t.Errorf("Name = %q, want fromenv", cfg.Name)
	}
	if cfg.Items[0] != "/opt/root/bin/app" {
		t.Errorf("Items[0] = %q, want /opt/root/bin/app", cfg.Items[0])
	}
	// {dst.*} and {platform} are left untouched by ExpandEnv (different prefix);
	// only the {env.*} value is substituted (forward slashes preserved verbatim).
	if cfg.Dirs[0].Path != `/opt/root\{dst.AppDataFolder}\x` {
		t.Errorf("Dirs[0].Path = %q, want /opt/root\\{dst.AppDataFolder}\\x", cfg.Dirs[0].Path)
	}
	if got := cfg.Services[0].Linux.Environment["TOKEN"]; got != "fromenv" {
		t.Errorf("environment TOKEN = %q, want fromenv", got)
	}
}

func TestExpandEnvErrorsOnMissingSortedDeduped(t *testing.T) {
	t.Setenv("ONEPKG_T2_SET", "v")
	cfg := &Config{
		Name:      "{env.ONEPKG_T2_SET}",      // present -> no error
		Version:   "{env.ONEPKG_T2_MISSING_B}", // missing
		OutputDir: "{env.ONEPKG_T2_MISSING_A}", // missing
	}
	err := cfg.ExpandEnv()
	if err == nil {
		t.Fatal("ExpandEnv should error on missing env vars")
	}
	if !strings.Contains(err.Error(), "ONEPKG_T2_MISSING_A") || !strings.Contains(err.Error(), "ONEPKG_T2_MISSING_B") {
		t.Errorf("error should list both missing vars, got: %v", err)
	}
	// Sorted: A before B in the message.
	if strings.Index(err.Error(), "ONEPKG_T2_MISSING_A") > strings.Index(err.Error(), "ONEPKG_T2_MISSING_B") {
		t.Errorf("missing vars should be sorted, got: %v", err)
	}
}

func TestLoadConfigExpandsEnv(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0755)
	os.WriteFile(filepath.Join(dir, "bin", "app"), []byte("x"), 0755)
	content := `
name = "envpkg"
version = "{env.ONEPKG_T2_VER}"
items = ["bin/app"]
`
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(content), 0644)

	t.Setenv("ONEPKG_T2_VER", "v9.9.9")
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Version != "v9.9.9" {
		t.Errorf("Version = %q, want v9.9.9 (env not expanded)", cfg.Version)
	}
}

func TestLoadConfigErrorsOnMissingEnv(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0755)
	os.WriteFile(filepath.Join(dir, "bin", "app"), []byte("x"), 0755)
	content := `
name = "envpkg"
version = "{env.ONEPKG_T2_DEFINITELY_UNSET}"
items = ["bin/app"]
`
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(content), 0644)

	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("loadConfig should error when a referenced env var is unset")
	}
	if !strings.Contains(err.Error(), "ONEPKG_T2_DEFINITELY_UNSET") {
		t.Errorf("error should name the missing var, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test -run 'TestExpandEnv|TestLoadConfig' -v`
Expected: FAIL / compile error — `cfg.ExpandEnv` undefined, `loadConfig` undefined.

- [ ] **Step 3: Create `cmd/onepkg/markers.go` with the walker and `ExpandEnv`**

```go
package main

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// envMarkerRe matches a {env.NAME} token. NAME is an OS environment variable
// name: a letter or underscore, then word characters.
var envMarkerRe = regexp.MustCompile(`\{env\.([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv replaces every {env.NAME} token in the config's string fields with the
// build host's value of os.Getenv(NAME). A referenced variable that is unset or
// empty is a configuration error: ExpandEnv collects all offenders and returns a
// single error listing them (deduped, sorted). {platform} and {dst.NAME} are left
// untouched (different prefixes), so they can coexist with {env.NAME} in one field.
func (c *Config) ExpandEnv() error {
	var missing []string
	seen := map[string]bool{}
	walkStrings(reflect.ValueOf(c), func(_ string, s *string) {
		expanded, miss := expandEnvString(*s)
		*s = expanded
		for _, name := range miss {
			if !seen[name] {
				seen[name] = true
				missing = append(missing, name)
			}
		}
	})
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("environment variables referenced via {env.NAME} are unset or empty: %s", strings.Join(missing, ", "))
	}
	return nil
}

// expandEnvString substitutes {env.NAME} in s. Returns the substituted string and
// the list of referenced variable names that were unset or empty (the marker is
// left in place in the returned string; the caller errors out before use).
func expandEnvString(s string) (string, []string) {
	var missing []string
	out := envMarkerRe.ReplaceAllStringFunc(s, func(tok string) string {
		name := envMarkerRe.FindStringSubmatch(tok)[1]
		if val := os.Getenv(name); val != "" {
			return val
		}
		missing = append(missing, name)
		return tok
	})
	return out, missing
}

// walkStrings visits every settable string field reachable from v (a *Config),
// invoking fn with a dotted field path (e.g. "Dirs[0].Path", "Services[0].Linux.Environment[KEY]")
// and a pointer to that field's value. It descends structs, pointers, slices, and
// maps; map keys and non-string scalars are skipped. Map values are substituted via
// copy-out/copy-in (map values are not addressable). Config has no map-of-struct, so
// the String branch is only reached through settable struct/slice paths.
//
// It is the shared traversal for {env.NAME} substitution (fn ignores the path) and
// stray {dst.NAME} validation (fn uses the path to allow-list supported fields).
func walkStrings(v reflect.Value, fn func(path string, s *string)) {
	walkStringsInto(v, "", fn)
}

func walkStringsInto(v reflect.Value, path string, fn func(path string, s *string)) {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		walkStringsInto(v.Elem(), path, fn)
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			child := t.Field(i).Name
			if path != "" {
				child = path + "." + child
			}
			walkStringsInto(v.Field(i), child, fn)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkStringsInto(v.Index(i), fmt.Sprintf("%s[%d]", path, i), fn)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			child := v.MapIndex(k)
			if child.Kind() != reflect.String {
				continue
			}
			cur := child.String()
			fn(fmt.Sprintf("%s[%s]", path, k.String()), &cur)
			v.SetMapIndex(k, reflect.ValueOf(cur))
		}
	case reflect.String:
		cur := v.String()
		fn(path, &cur)
		v.SetString(cur)
	}
}
```

- [ ] **Step 4: Add the `loadConfig` seam to `main.go` and call it from `main()`**

In `cmd/onepkg/main.go`, add this helper (place it just above `func BuildForPlatform`):

```go
// loadConfig parses config.toml, expands {env.NAME} host variables, and applies
// defaults. It is the testable seam for the {env.NAME} wiring (main() otherwise
// orchestrates inline). Validate is called separately by main() so its error
// message and the output-override step are preserved.
func loadConfig(configPath string) (*Config, error) {
	cfg, err := ParseConfig(configPath)
	if err != nil {
		return nil, err
	}
	if err := cfg.ExpandEnv(); err != nil {
		return nil, err
	}
	cfg.ApplyDefaults()
	return cfg, nil
}
```

Then in `main()`, replace the ParseConfig + ApplyDefaults block (current lines 106-112):

```go
	cfg, err := ParseConfig(configPath)
	if err != nil {
		fmt.Printf("Error parsing config: %v\n", err)
		os.Exit(1)
	}

	cfg.ApplyDefaults()
```

with:

```go
	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
```

(The `outputOverride` block immediately after is unchanged.)

- [ ] **Step 5: Run the new tests**

Run: `cd cmd/onepkg && go test -run 'TestExpandEnv|TestLoadConfig' -v`
Expected: PASS — all four tests green.

- [ ] **Step 6: Run the full suite**

Run: `make test`
Expected: GREEN — no regressions; existing tests that call `ParseConfig`+`ApplyDefaults`+`Validate` directly are unaffected (they bypass `loadConfig`).

- [ ] **Step 7: Commit**

```bash
git add cmd/onepkg/markers.go cmd/onepkg/main.go cmd/onepkg/config_test.go
git commit -m "feat(markers): add build-time {env.NAME} host-variable substitution

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: Fail fast on stray `{dst.NAME}` markers

Reuse the Task 2 walker to reject `{dst.NAME}` anywhere it will not be resolved (everywhere except `dirs[].path`, `system_overlay[].system`, run `commands[].args`, run `commands[].shell`).

**Files:**
- Modify: `cmd/onepkg/markers.go` (add `dstMarkerRe`, `validateNoStrayDstMarkers`, `isDstSupportedField`)
- Modify: `cmd/onepkg/config.go` (call it from `Validate`)
- Modify: `cmd/onepkg/config_test.go` (add tests)

**Interfaces:**
- Consumes: `walkStrings` (from Task 2).
- Produces: `(*Config) validateNoStrayDstMarkers() error`.

- [ ] **Step 1: Write the failing tests in `config_test.go`**

Append to `cmd/onepkg/config_test.go`:

```go
func TestValidateRejectsStrayDstMarkerInItems(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0755)
	os.WriteFile(filepath.Join(dir, "bin", "app"), []byte("x"), 0755)
	content := `
name = "x"
version = "v1"
items = ["bin/app", "{dst.AppDataFolder}/cfg"]
`
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content))
	cfg, _ := ParseConfig(filepath.Join(dir, "config.toml"))
	cfg.ApplyDefaults()
	err := cfg.Validate(dir)
	if err == nil {
		t.Fatal("Validate should reject {dst.NAME} in items[]")
	}
	if !strings.Contains(err.Error(), "{dst.NAME}") {
		t.Errorf("error should explain {dst.NAME} placement, got: %v", err)
	}
}

func TestValidateRejectsStrayDstMarkerInName(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0755)
	os.WriteFile(filepath.Join(dir, "bin", "app"), []byte("x"), 0755)
	content := `
name = "{dst.AppDataFolder}"
version = "v1"
items = ["bin/app"]
`
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content))
	cfg, _ := ParseConfig(filepath.Join(dir, "config.toml"))
	cfg.ApplyDefaults()
	if err := cfg.Validate(dir); err == nil {
		t.Fatal("Validate should reject {dst.NAME} in name")
	}
}

func TestValidateAllowsDstMarkerInRunShellAndArgs(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "bin"), 0755)
	os.WriteFile(filepath.Join(dir, "bin", "app.exe"), []byte("x"), 0755)
	content := `
name = "x"
version = "v1"
mode = "run"
items = ["bin/app.exe"]

[[commands]]
exec = "bin/app.exe"
args = ["--config", "{dst.AppDataFolder}\\cfg.json"]

[[commands]]
shell = "Copy-Item p \"{dst.AppDataFolder}\\x\""
`
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content))
	cfg, _ := ParseConfig(filepath.Join(dir, "config.toml"))
	cfg.ApplyDefaults()
	if err := cfg.Validate(dir); err != nil {
		t.Fatalf("Validate should allow {dst.NAME} in run args and shell: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test -run 'TestValidateRejectsStrayDst|TestValidateAllowsDstMarkerInRunShell' -v`
Expected: FAIL — the two reject-tests see no error (stray check not implemented yet); the allow-test passes already (it would pass today too, but it pins the allow-list behavior).

- [ ] **Step 3: Implement the stray check in `markers.go`**

Append to `cmd/onepkg/markers.go`:

```go
// dstMarkerRe matches any {dst. token (known or unknown name). Used by the stray
// check to catch {dst.NAME} used in fields that do not resolve destination markers.
var dstMarkerRe = regexp.MustCompile(`\{dst\.`)

// dstSupportedFieldRe matches the dotted field paths of the four field types that
// resolve {dst.NAME} at install/run time: dirs[].path, system_overlay[].system,
// and run-mode commands[].args / commands[].shell.
var dstSupportedFieldRe = regexp.MustCompile(`^(Dirs\[\d+\]\.Path|SystemOverlay\[\d+\]\.System|Commands\[\d+\]\.Args\[\d+\]|Commands\[\d+\]\.Shell)$`)

func isDstSupportedField(path string) bool {
	return dstSupportedFieldRe.MatchString(path)
}

// validateNoStrayDstMarkers errors if any {dst.NAME} marker appears in a string
// field that is not one of the four supported types. A {dst.NAME} elsewhere would
// survive literally into the package, so fail fast.
func (c *Config) validateNoStrayDstMarkers() error {
	var offenders []string
	walkStrings(reflect.ValueOf(c), func(path string, s *string) {
		if dstMarkerRe.MatchString(*s) && !isDstSupportedField(path) {
			offenders = append(offenders, path)
		}
	})
	if len(offenders) > 0 {
		sort.Strings(offenders)
		return fmt.Errorf("{dst.NAME} markers are only valid in dirs[].path, system_overlay[].system, and run commands[].args/shell; found in: %s", strings.Join(offenders, ", "))
	}
	return nil
}
```

- [ ] **Step 4: Call it from `Validate` in `config.go`**

In `cmd/onepkg/config.go`, inside `func (c *Config) Validate`, add the call right after the mode `switch` block (after current line 169, before the items loop). Insert:

```go
	// {dst.NAME} is only resolvable in path/args/shell fields; reject it anywhere else.
	if err := c.validateNoStrayDstMarkers(); err != nil {
		return err
	}
```

- [ ] **Step 5: Run the new tests**

Run: `cd cmd/onepkg && go test -run 'TestValidateRejectsStrayDst|TestValidateAllowsDstMarkerInRunShell' -v`
Expected: PASS.

- [ ] **Step 6: Run the full suite**

Run: `make test`
Expected: GREEN. The Task 1 tests that place `{dst.NAME}` in `dirs[].path`, `system_overlay[].system`, and run `args` (`TestValidateKnownFolderInDirsAndOverlay`, `TestValidateRunArgsKnownFolder`) still pass — those paths are on the allow-list.

- [ ] **Step 7: Commit**

```bash
git add cmd/onepkg/markers.go cmd/onepkg/config.go cmd/onepkg/config_test.go
git commit -m "feat(markers): fail fast on {dst.NAME} markers outside resolvable fields

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 4: Documentation

Update `docs/config-example.toml` to the `{dst.NAME}` form everywhere, add an `{env.NAME}` example, and document the three namespaces in `CLAUDE.md`.

**Files:**
- Modify: `docs/config-example.toml`
- Modify: `CLAUDE.md`

**Interfaces:** None (docs only).

- [ ] **Step 1: Update `docs/config-example.toml`**

In the "Windows known-folder variables" comment block (lines ~167-212):
- Change the Syntax comment from:
  ```
  # Syntax:
  #   {VarName}  in path-string fields: dirs[].path, system_overlay[].system,
  #              and run-mode commands[].args
  #   $VarName   in run-mode commands[].shell (ordinary PowerShell variable)
  ```
  to:
  ```
  # Syntax:
  #   {dst.VarName}  in path-string fields: dirs[].path, system_overlay[].system,
  #                  and run-mode commands[].args AND commands[].shell
  ```
- Line 198: `# path = "{AppDataFolder}\\MyApp\\data"` → `# path = "{dst.AppDataFolder}\\MyApp\\data"`.
- Line 203: `# system = "{ProgramFiles64Folder}\\OtherApp\\config.json"` → `# system = "{dst.ProgramFiles64Folder}\\OtherApp\\config.json"`.
- Line 209: `# args = ["--config", "{AppDataFolder}\\MyApp\\config.json"]` → `# args = ["--config", "{dst.AppDataFolder}\\MyApp\\config.json"]`.
- Line 212: `# shell = "Copy-Item profile.json \"$AppDataFolder\\MyApp\\\""` → `# shell = "Copy-Item profile.json \"{dst.AppDataFolder}\\MyApp\\\""`.

Then add a new block after the known-folder block (after line 212):

```toml
# --- Host environment variables (build-time, all platforms) ---
# {env.NAME} is replaced at BUILD time with the build host's value of the
# environment variable NAME, in EVERY string field (name, version, paths,
# args, etc.). It produces a literal in the packed config. A referenced
# variable that is unset or empty fails the build (no defaults).
#
# This is distinct from {dst.NAME} (resolved on the Windows target at
# install/run time) and from the legacy {platform} marker.
#
# version = "{env.BUILD_VERSION}"
# output_dir = "{env.DIST_DIR}"
```

- [ ] **Step 2: Update `CLAUDE.md`**

Replace the existing "Windows known-folder variables" Key Convention bullet (the one beginning "**Windows known-folder variables:** `{VarName}` ...") with:

```markdown
- **Config marker namespaces:** three `{...}` markers, mutually exclusive by prefix:
  - `{platform}` — legacy bare marker, build-time in Go, for `items[]` (unchanged).
  - `{env.NAME}` — build-time host-environment substitution via `os.Getenv`, applied to every string field after parse; unset/empty → build-failing error listing offenders. Implemented in `cmd/onepkg/markers.go` (`(*Config).ExpandEnv`), wired through `loadConfig`. No defaults.
  - `{dst.NAME}` — Windows known-folder variables, resolved on the TARGET at install/run time in generated PowerShell via `[Environment]::GetFolderPath` (never in Go). Valid only in `dirs[].path`, `system_overlay[].system`, and run-mode `commands[].args` / `commands[].shell`; anywhere else is a validation error. Curated set in `installer.KnownFolders`. Windows-only (marked `dirs`/`overlay` rejected on non-Windows builds). Design spec: `docs/superpowers/specs/2026-07-14-config-marker-namespaces-design.md`.
```

- [ ] **Step 3: Commit**

```bash
git add docs/config-example.toml CLAUDE.md
git commit -m "docs: document {env.NAME}/{dst.NAME}/{platform} marker namespaces

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Self-Review Notes

**Spec coverage:** `{env.NAME}` host substitution → Task 2; `{dst.NAME}` rename → Task 1; run-shell unification (retire `$Name`) → Task 1 (runner uses `ReplaceMarkers`); shared build-time engine (reflection walk) → Task 2 `walkStrings`; fail-fast on stray `{dst.NAME}` → Task 3; build-time Windows-only guard → unchanged (covered by existing `main_test.go`, renamed in Task 1); backward-compat unmarked output → covered by existing `NoKnownFolderDefsWhenUnmarked`-style tests (unchanged expectations); docs → Task 4. No spec section unaddressed.

**Type consistency:** `ReplaceMarkers` (Task 1) and `walkStrings`/`ExpandEnv`/`validateNoStrayDstMarkers` (Tasks 2-3) names are used consistently. The allow-list regex field paths (`Dirs[0].Path` etc.) match the paths `walkStrings` emits (root struct field names = Go field names `Dirs`/`SystemOverlay`/`Commands`; `.Path`/`.System`/`.Args[i]`/`.Shell`).

**Known limitations carried in spec, not re-tested:** actual `[Environment]::GetFolderPath` values can't run on macOS (existing Windows-test gap); a bare legacy `{AppDataFolder}` left in a *path* field is treated as a literal (documented, not detected — the stray check keys on the `{dst.}` prefix).
