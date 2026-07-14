# Windows Run-Commands Package Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Windows-only `mode = "run"` package type that produces a double-clickable `.exe` which runs a list of commands (bundled executables and/or arbitrary PowerShell) once, in order, without installing anything.

**Architecture:** New `mode`/`[[commands]]` config fields route `BuildForWindows` to a new `BuildForWindowsRun`, which zips the payload (no `VERSION` file), generates a lean `run.ps1` via a new `installer.GeneratePowerShellRunner`, and reuses the existing `GenerateExeWrapper` (stub + ps1 + zip + manifest + magic, **unchanged**). The stub is NOT modified: `run.ps1` declares (and ignores) the `-InstallDir`/`-Action` params the stub forwards, so one stub serves both install and run packages and every existing install behavior is preserved.

**Tech Stack:** Go 1.21, module `onepkg`, `github.com/pelletier/go-toml`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-07-13-windows-run-commands-design.md`

## Global Constraints

- **Go 1.21**, module name `onepkg`. TOML dep is `github.com/pelletier/go-toml` (already imported in `config.go`).
- **Package layout:** `cmd/onepkg/*.go` is `package main`; `cmd/onepkg/installer/` is `package installer`.
- **Windows stubs are gitignored build artifacts** (`cmd/onepkg/installer/stubs/*.exe`). Before ANY `go test`/`go build` under `cmd/onepkg`, run `make win-wrapper` once — the `//go:embed stubs/*.exe` directives in `installer/exewrapper.go` fail otherwise. **This feature does NOT modify the stubs.**
- **TDD:** write the failing test first, then implement. Every function gets a test.
- **Path/naming rules:** `{os}-{arch}` platforms (e.g. `windows-amd64`); filenames `{name}_{version}_{os}_{arch}.exe`. Arch map: `unameArch("windows","amd64")="AMD64"`, `unameArch("windows","arm64")="ARM64"`.
- **Commit frequently:** one commit per task, message prefix `feat(windows-run):`, `refactor(installer):`, `docs:`, etc.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `cmd/onepkg/config.go` | Add `Mode`/`Commands`/`RequireAdmin` + `CommandConfig`; `ApplyDefaults` default; `Validate` mode-branch; new `itemMatches` helper. |
| `cmd/onepkg/config_test.go` | Run-mode parse/defaults/validation tests. |
| `cmd/onepkg/installer/powershell.go` | **Refactor only:** extract `powershellCheckAdmin`, `powershellCheckArch`, `powershellArchVars` shared helpers; installer uses them. |
| `cmd/onepkg/installer/powershell_runner.go` (new) | `GeneratePowerShellRunner`, `PowerShellRunnerConfig`, `CommandSpec`. |
| `cmd/onepkg/installer/powershell_runner_test.go` (new) | Runner-generation tests. |
| `cmd/onepkg/main.go` | `BuildForPlatform` non-Windows guard; `BuildForWindows` mode branch; new `BuildForWindowsRun`. |
| `cmd/onepkg/main_test.go` | `BuildForWindowsRun` + guard tests. |
| `docs/config-example.toml` | `mode = "run"` example. |
| `CLAUDE.md` | Document `mode = "run"` and `[[commands]]`. |

---

### Task 1: Baseline — generate stubs and confirm the suite is green

**Files:** none modified (setup only)

This guarantees the embedded stubs exist and the full suite passes **before** any change, so later breakage is unambiguous.

- [ ] **Step 1: Generate the Windows stubs (one-time per checkout)**

Run: `make win-wrapper`
Expected: two files created: `cmd/onepkg/installer/stubs/wrapper-stub-amd64.exe` and `cmd/onepkg/installer/stubs/wrapper-stub-arm64.exe` (gitignored — do NOT commit them).

- [ ] **Step 2: Run the whole suite**

Run: `make test`
Expected: PASS. (`make test` regenerates stubs first, then runs `go test ./...` in `cmd/onepkg` and `go test` in `stub-src`.) If anything fails here, stop — the baseline is broken and must be fixed before proceeding.

- [ ] **Step 3: No commit** (no source changes).

---

### Task 2: Config — add run-mode fields + ApplyDefaults default

**Files:**
- Modify: `cmd/onepkg/config.go` (struct `Config` ~line 12; `ApplyDefaults` ~line 82)
- Test: `cmd/onepkg/config_test.go`

**Interfaces:**
- Produces: `Config.Mode string` (`toml:"mode"`), `Config.Commands []CommandConfig` (`toml:"commands"`), `Config.RequireAdmin bool` (`toml:"require_admin"`); type `CommandConfig{ Exec, Args, Shell }`. `ApplyDefaults` sets `Mode = "install"` when empty.

- [ ] **Step 1: Write the failing tests** (append to `cmd/onepkg/config_test.go`)

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test -run 'TestParseConfigRunMode|TestApplyDefaultsMode' -v`
Expected: FAIL / compile error (`cfg.Mode` / `cfg.Commands` / `cfg.RequireAdmin` unknown, `CommandConfig` undefined).

- [ ] **Step 3: Add the struct fields and type**

In `cmd/onepkg/config.go`, add the `CommandConfig` type (place it right after the `Config` struct, before `ServiceConfig`):

```go
// CommandConfig is one entry in [[commands]] for mode = "run" packages.
// Exactly one of Exec or Shell must be set.
type CommandConfig struct {
	Exec  string   `toml:"exec"`  // bundled file path, resolved against the temp extraction dir; empty if Shell set
	Args  []string `toml:"args"`  // args for Exec
	Shell string   `toml:"shell"` // arbitrary PowerShell expression; empty if Exec set
}
```

Add three fields to the `Config` struct (after `Services`):

```go
	Mode         string          `toml:"mode"`          // "install" (default) | "run"
	Commands     []CommandConfig `toml:"commands"`
	RequireAdmin bool            `toml:"require_admin"` // run mode only; default false (zero value)
```

- [ ] **Step 4: Add the ApplyDefaults default**

In `ApplyDefaults` (in `cmd/onepkg/config.go`), add at the very top of the function body (before the `OutputDir` check):

```go
	if c.Mode == "" {
		c.Mode = "install"
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd cmd/onepkg && go test -run 'TestParseConfigRunMode|TestApplyDefaultsMode' -v`
Expected: PASS.

- [ ] **Step 6: Run the full package to confirm no regressions**

Run: `cd cmd/onepkg && go test ./...`
Expected: PASS (stubs already generated in Task 1).

- [ ] **Step 7: Commit**

```bash
git add cmd/onepkg/config.go cmd/onepkg/config_test.go
git commit -m "feat(windows-run): add mode/commands/require_admin config fields"
```

---

### Task 3: Config — Validate run-mode and install-mode rules + itemMatches

**Files:**
- Modify: `cmd/onepkg/config.go` (`Validate` ~line 136; add `itemMatches`)
- Test: `cmd/onepkg/config_test.go`

**Interfaces:**
- Consumes: `HasPlatformMarker`, `StripPlatformMarker` (already in `config.go`).
- Produces: `itemMatches(items []string, exec string) bool` — strict exact-or-stripped-`{platform}` match (NO prefix match). `Validate` now branches on `Mode`.

**Validation rules implemented (from spec §Validation):**
- Run mode: ≥1 command; each command exactly one of `exec`/`shell`; `exec` must `itemMatches` an item and must NOT contain `{platform}`; `items` optional; `services`/`dirs`/`system_overlay` rejected.
- Install mode: `[[commands]]` rejected; existing item-required + service/dirs/overlay checks unchanged.

- [ ] **Step 1: Write the failing tests** (append to `cmd/onepkg/config_test.go`)

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test -run 'TestValidateRunMode|TestValidateInstallModeRejectsCommands|TestValidateBadMode' -v`
Expected: FAIL (current `Validate` still requires items unconditionally, accepts `[[commands]]`, and has no mode branch).

- [ ] **Step 3: Add the `itemMatches` helper**

Append to `cmd/onepkg/config.go` (near `StripPlatformMarker`):

```go
// itemMatches reports whether exec names a bundled item, by exact match or by the
// stripped {platform} form (e.g. "bin/cleanup" matches item "bin/cleanup.{platform}").
// It deliberately does NOT use service-exec's looser prefix match (config.go Validate).
func itemMatches(items []string, exec string) bool {
	for _, item := range items {
		if item == exec {
			return true
		}
		if HasPlatformMarker(item) && StripPlatformMarker(item) == exec {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Rewrite `Validate` to branch on mode**

Replace the **entire body** of `func (c *Config) Validate(configDir string) error` in `cmd/onepkg/config.go` with:

```go
func (c *Config) Validate(configDir string) error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if c.Version == "" {
		return fmt.Errorf("version is required")
	}

	mode := c.Mode
	if mode == "" {
		mode = "install"
	}
	switch mode {
	case "install", "run":
		// valid
	default:
		return fmt.Errorf("mode %q must be \"install\" or \"run\"", c.Mode)
	}

	// Item existence / traversal checks apply to items present in either mode.
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

	if mode == "run" {
		return c.validateRun()
	}
	return c.validateInstall(configDir)
}

// validateInstall holds the install-mode rules: items required, [[commands]]
// rejected, plus service/dirs/overlay checks (unchanged from pre-run-mode behavior).
func (c *Config) validateInstall(configDir string) error {
	if len(c.Items) == 0 {
		return fmt.Errorf("at least one item is required")
	}
	if len(c.Commands) > 0 {
		return fmt.Errorf("[[commands]] is only allowed in mode = \"run\"")
	}

	itemSet := make(map[string]bool, len(c.Items))
	for _, item := range c.Items {
		itemSet[item] = true
	}
	for _, svc := range c.Services {
		if svc.Name != "" && !isValidName(svc.Name) {
			return fmt.Errorf("service name %q must contain only alphanumeric characters, hyphens, and underscores", svc.Name)
		}
		if svc.Exec != "" && HasPlatformMarker(svc.Exec) {
			return fmt.Errorf("service exec %q should not contain {platform} marker (use stripped path, e.g., \"bin/myapp\")", svc.Exec)
		}
		if svc.Exec != "" && !itemSet[svc.Exec] {
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
		if svc.Windows.StartType != "" {
			switch svc.Windows.StartType {
			case "auto", "manual", "disabled":
			default:
				return fmt.Errorf("service windows start_type %q must be one of: auto, manual, disabled", svc.Windows.StartType)
			}
		}
		if svc.Windows.RecoveryDelayMs != nil && *svc.Windows.RecoveryDelayMs < 0 {
			return fmt.Errorf("service windows recovery_delay_ms must be >= 0")
		}
	}
	for _, d := range c.Dirs {
		if d.Path == "" {
			return fmt.Errorf("dirs path is required")
		}
		if strings.Contains(d.Path, "..") {
			return fmt.Errorf("path traversal not allowed in dirs path %q", d.Path)
		}
		cleanPath := filepath.Clean(d.Path)
		if filepath.IsAbs(d.Path) && isCriticalPath(cleanPath) {
			return fmt.Errorf("critical system path not allowed: %q", d.Path)
		}
		if d.Mode != "" && !isValidMode(d.Mode) {
			return fmt.Errorf("invalid mode %q for dirs path %q (must be octal like 0755)", d.Mode, d.Path)
		}
		if d.Owner != "" && !isValidName(d.Owner) {
			return fmt.Errorf("invalid owner %q for dirs path %q", d.Owner, d.Path)
		}
		if d.Group != "" && !isValidName(d.Group) {
			return fmt.Errorf("invalid group %q for dirs path %q", d.Group, d.Path)
		}
	}
	for _, overlay := range c.SystemOverlay {
		if overlay.Item == "" {
			return fmt.Errorf("overlay item is required")
		}
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
		matched := false
		for _, item := range c.Items {
			if overlay.Item == item {
				matched = true
				break
			}
			if HasPlatformMarker(item) && StripPlatformMarker(item) == overlay.Item {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("overlay item %q not found in items", overlay.Item)
		}
	}
	return nil
}

// validateRun holds the run-mode rules: install artifacts rejected, commands
// required and well-formed, command exec must reference a bundled item.
func (c *Config) validateRun() error {
	if len(c.Services) > 0 {
		return fmt.Errorf("[[services]] is not allowed in mode = \"run\"")
	}
	if len(c.Dirs) > 0 {
		return fmt.Errorf("[[dirs]] is not allowed in mode = \"run\"")
	}
	if len(c.SystemOverlay) > 0 {
		return fmt.Errorf("[[system_overlay]] is not allowed in mode = \"run\"")
	}
	if len(c.Commands) == 0 {
		return fmt.Errorf("mode = \"run\" requires at least one [[commands]] entry")
	}
	for i, cmd := range c.Commands {
		hasExec := cmd.Exec != ""
		hasShell := cmd.Shell != ""
		if hasExec == hasShell {
			return fmt.Errorf("command %d must set exactly one of exec or shell", i)
		}
		if hasExec {
			if HasPlatformMarker(cmd.Exec) {
				return fmt.Errorf("command %d exec %q should not contain {platform} marker (use stripped path)", i, cmd.Exec)
			}
			if !itemMatches(c.Items, cmd.Exec) {
				return fmt.Errorf("command %d exec %q not found in items", i, cmd.Exec)
			}
		}
	}
	return nil
}
```

- [ ] **Step 5: Run the validation tests to verify they pass**

Run: `cd cmd/onepkg && go test -run 'TestValidateRunMode|TestValidateInstallModeRejectsCommands|TestValidateBadMode' -v`
Expected: PASS.

- [ ] **Step 6: Run the whole package to confirm install-mode validation still works**

Run: `cd cmd/onepkg && go test ./...`
Expected: PASS (all pre-existing install/overlay/service tests still green — they exercise `validateInstall`).

- [ ] **Step 7: Commit**

```bash
git add cmd/onepkg/config.go cmd/onepkg/config_test.go
git commit -m "feat(windows-run): validate run-mode and install-mode command rules"
```

---

### Task 4: Refactor — extract shared PowerShell helpers (behavior-preserving)

**Goal:** Pull `CheckAdmin`, `CheckArch`, and the arch-variable emission out of `GeneratePowerShellInstaller`'s monolithic blob into small helpers, so the new runner (Task 5) can reuse them without duplicating PowerShell. **Existing installer tests MUST stay green** — the extracted text is byte-identical except the CheckAdmin hint, which no test asserts.

**Files:**
- Modify: `cmd/onepkg/installer/powershell.go`
- Test: existing `cmd/onepkg/installer/powershell_test.go` (unchanged — it is the regression gate)

**Interfaces:**
- Produces (package `installer`): `powershellCheckAdmin() string`, `powershellCheckArch() string`, `powershellArchVars(expectedArch string) string`. (Unexported; used by both `GeneratePowerShellInstaller` and `GeneratePowerShellRunner`.)

- [ ] **Step 1: Add the three helper functions**

Add to `cmd/onepkg/installer/powershell.go` (e.g. right after the `psQuote` function):

```go
// powershellCheckArchVars emits the $EXPECTED_ARCH and $ALLOWED_ARCHS variable
// block that CheckArch reads. An amd64 package also runs on arm64 Windows via
// x64 emulation; an arm64 package only runs on arm64.
func powershellArchVars(expectedArch string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "$EXPECTED_ARCH = %s\n", psQuote(expectedArch))
	allowedArches := []string{expectedArch}
	if expectedArch == "AMD64" {
		allowedArches = append(allowedArches, "ARM64")
	}
	b.WriteString("$ALLOWED_ARCHS = @(")
	for i, a := range allowedArches {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(psQuote(a))
	}
	b.WriteString(")\n")
	return b.String()
}

// powershellCheckAdmin emits the CheckAdmin function (admin-gate check; does not elevate).
func powershellCheckAdmin() string {
	return `function CheckAdmin {
    $currentUser = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]$currentUser
    $isAdmin = $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    if (-not $isAdmin) {
        Write-Error "This script requires administrator privileges."
        Write-Host "Re-run from an elevated PowerShell prompt."
        exit 1
    }
}

`
}

// powershellCheckArch emits the CheckArch function (references $EXPECTED_ARCH / $ALLOWED_ARCHS).
func powershellCheckArch() string {
	return `function CheckArch {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($ALLOWED_ARCHS -notcontains $arch) {
        Write-Error "Error: This package is for $EXPECTED_ARCH (supported: $($ALLOWED_ARCHS -join ', ')), current system is $arch"
        exit 1
    }
}

`
}
```

> Note: the only intentional change vs. the original is the CheckAdmin hint line (was `Run with: … -Action install`), which no existing test asserts. Both install and run packages now print the same generic hint.

- [ ] **Step 2: Make `GeneratePowerShellInstaller` use the helpers**

In `GeneratePowerShellInstaller`, replace the inline arch-variable emission block (the lines that build `$EXPECTED_ARCH` and `$ALLOWED_ARCHS` — currently the `$EXPECTED_ARCH` Fprintf, the `allowedArches` computation, and the `$ALLOWED_ARCHS = @(...)` loop) with a single call:

```go
	b.WriteString(powershellArchVars(cfg.ExpectedArch))
```

Then in `powershellFunctions`, replace the leading `CheckAdmin`+`CheckArch` portion of the big raw-string literal with helper calls, so `powershellFunctions` begins:

```go
func powershellFunctions(cfg PowerShellConfig) string {
	hasService := cfg.ServiceName != ""
	var b strings.Builder

	b.WriteString(powershellCheckAdmin())
	b.WriteString(powershellCheckArch())

	if hasService {
		b.WriteString(`function InstallService {
    ...existing InstallService text unchanged...
`)

`)
	}

	b.WriteString(`function CreateDirs {
    ...existing CreateDirs/ApplyOverlays/Install/Uninstall/ShowVersion/ListFiles/VerifyFiles text unchanged...
`)

`)
	return b.String()
}
```

Concretely: delete the `function CheckAdmin { … }` and `function CheckArch { … }` blocks from the start of the existing raw string, and insert the two `b.WriteString(powershellCheckAdmin())` / `b.WriteString(powershellCheckArch())` calls in their place. Leave every other function (`InstallService`, `UninstallService`, `CreateDirs`, `ApplyOverlays`, `Install`, `Uninstall`, `ShowVersion`, `ListFiles`, `VerifyFiles`) byte-for-byte unchanged.

- [ ] **Step 3: Run the existing installer tests — they must still pass**

Run: `cd cmd/onepkg && go test ./installer/ -run TestGeneratePowerShellInstaller -v`
Expected: PASS. This is the regression gate: assertions like `$EXPECTED_ARCH = 'AMD64'`, `$ALLOWED_ARCHS = @('AMD64', 'ARM64')`, `function CheckAdmin`, `function CheckArch`, `$ALLOWED_ARCHS -notcontains $arch` must all still match.

- [ ] **Step 4: Run the full suite**

Run: `make test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/onepkg/installer/powershell.go
git commit -m "refactor(installer): extract shared CheckAdmin/CheckArch/arch-var helpers"
```

---

### Task 5: New runner — `installer.GeneratePowerShellRunner`

**Files:**
- Create: `cmd/onepkg/installer/powershell_runner.go`
- Test: `cmd/onepkg/installer/powershell_runner_test.go`

**Interfaces:**
- Consumes (from Task 4): `powershellCheckAdmin()`, `powershellCheckArch()`, `powershellArchVars(expectedArch)`, `psQuote(s)`.
- Produces (package `installer`): `GeneratePowerShellRunner(cfg PowerShellRunnerConfig) (string, error)`, `PowerShellRunnerConfig`, `CommandSpec`.

**Runner output shape (`run.ps1`):** declares `$PayloadDir`/`$InstallDir`/`$Action` (last two ignored — the unchanged stub forwards them); emits arch vars + `CheckArch` always, `CheckAdmin` iff `RequireAdmin`; extracts `payload.zip` iff `HasPayload`; runs each command (exec via `& (Join-Path $PayloadDir '<exec>') '<arg>'…`, shell via `Invoke-Expression '<shell>'`) with fail-fast; ends with `Read-Host 'Press Enter to exit'`.

> **Correctness note (shell failure detection):** a shell command resets `$LASTEXITCODE = 0` before `Invoke-Expression`, then fails if `(-not $?) -or ($LASTEXITCODE -ne 0)`. The reset matters: without it, `$LASTEXITCODE` is `$null` for a cmdlet-only shell (the first command, or after no native exe) and `$null -ne 0` is `$true` in PowerShell, causing a false failure. The reset makes `$LASTEXITCODE` catch native-exe failures while `$?` catches cmdlet/expression failures.

- [ ] **Step 1: Write the failing tests** (new file `cmd/onepkg/installer/powershell_runner_test.go`)

```go
package installer

import (
	"strings"
	"testing"
)

func TestGeneratePowerShellRunner(t *testing.T) {
	cfg := PowerShellRunnerConfig{
		Name:         "cleanup",
		Version:      "v1.0.0",
		Platform:     "windows-amd64",
		ExpectedArch: "AMD64",
		RequireAdmin: true,
		HasPayload:   true,
		Commands: []CommandSpec{
			{Exec: "bin/cleanup", Args: []string{"--all", "--force"}},
			{Shell: "netsh advfirewall set allprofiles state off"},
		},
	}

	out, err := GeneratePowerShellRunner(cfg)
	if err != nil {
		t.Fatalf("GeneratePowerShellRunner: %v", err)
	}

	mustContain := []string{
		"param(",
		"[string]$PayloadDir",
		"[string]$InstallDir",  // declared for stub parity, ignored
		"[string]$Action",      // declared for stub parity, ignored
		"$EXPECTED_ARCH = 'AMD64'",
		"$ALLOWED_ARCHS = @('AMD64', 'ARM64')",
		"function CheckArch",
		"function CheckAdmin", // RequireAdmin = true
		"Expand-Archive",      // HasPayload = true
		"& (Join-Path $PayloadDir 'bin/cleanup')",
		"'--all'",
		"'--force'",
		"Invoke-Expression 'netsh advfirewall set allprofiles state off'",
		"$LASTEXITCODE = 0", // shell reset before Invoke-Expression
		"exit 1",            // fail-fast present
		"Read-Host 'Press Enter to exit'",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q", s)
		}
	}
}

func TestGeneratePowerShellRunnerConditionalBlocks(t *testing.T) {
	// Shell-only package: no payload, no admin.
	cfg := PowerShellRunnerConfig{
		Name:         "shellonly",
		Version:      "v1.0.0",
		Platform:     "windows-arm64",
		ExpectedArch: "ARM64",
		RequireAdmin: false,
		HasPayload:   false,
		Commands:     []CommandSpec{{Shell: "Write-Host hi"}},
	}
	out, err := GeneratePowerShellRunner(cfg)
	if err != nil {
		t.Fatalf("GeneratePowerShellRunner: %v", err)
	}

	if strings.Contains(out, "function CheckAdmin") {
		t.Error("CheckAdmin should be omitted when RequireAdmin is false")
	}
	if strings.Contains(out, "Expand-Archive") {
		t.Error("Expand-Archive should be omitted when HasPayload is false")
	}
	if !strings.Contains(out, "$ALLOWED_ARCHS = @('ARM64')") {
		t.Error("arm64 package should allow only ARM64")
	}
}

func TestGeneratePowerShellRunnerIsLean(t *testing.T) {
	cfg := PowerShellRunnerConfig{
		Name: "x", Version: "v1", Platform: "windows-amd64", ExpectedArch: "AMD64",
		Commands: []CommandSpec{{Shell: "Write-Host hi"}},
	}
	out, _ := GeneratePowerShellRunner(cfg)
	// Must not carry install-only machinery.
	for _, bad := range []string{"New-Service", "switch ($Action)", "function Install", "function Uninstall"} {
		if strings.Contains(out, bad) {
			t.Errorf("runner output should not contain install-only %q", bad)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test ./installer/ -run TestGeneratePowerShellRunner -v`
Expected: FAIL / compile error (`GeneratePowerShellRunner`, `PowerShellRunnerConfig`, `CommandSpec` undefined).

- [ ] **Step 3: Implement the runner** (new file `cmd/onepkg/installer/powershell_runner.go`)

```go
package installer

import (
	"fmt"
	"strings"
)

// PowerShellRunnerConfig configures the generated run.ps1 script.
type PowerShellRunnerConfig struct {
	Name         string
	Version      string
	Platform     string
	ExpectedArch string // AMD64 or ARM64
	RequireAdmin bool
	HasPayload   bool // emit extraction block iff true
	Commands     []CommandSpec
}

// CommandSpec is one command for run.ps1. Exactly one of Exec or Shell is set.
type CommandSpec struct {
	Exec  string   // non-empty -> run bundled file
	Args  []string
	Shell string   // non-empty -> run PowerShell expression
}

// GeneratePowerShellRunner produces a lean run.ps1 that runs the configured
// commands once, in order, fail-fast. It reuses the shared CheckAdmin/CheckArch/
// arch-var helpers and declares the -InstallDir/-Action params the (unchanged)
// stub forwards, ignoring them.
func GeneratePowerShellRunner(cfg PowerShellRunnerConfig) (string, error) {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s %s run-commands package for %s\n", cfg.Name, cfg.Version, cfg.Platform)
	b.WriteString("# Generated by onepkg\n\n")

	b.WriteString("param(\n")
	b.WriteString("    [string]$PayloadDir = '',\n")
	b.WriteString("    [string]$InstallDir = '',    # accepted for stub parity; ignored in run mode\n")
	b.WriteString("    [string]$Action = 'install'  # accepted for stub parity; ignored in run mode\n")
	b.WriteString(")\n\n")

	fmt.Fprintf(&b, "$PKG_NAME = %s\n", psQuote(cfg.Name))
	fmt.Fprintf(&b, "$PKG_VERSION = %s\n", psQuote(cfg.Version))
	fmt.Fprintf(&b, "$PKG_PLATFORM = %s\n", psQuote(cfg.Platform))
	b.WriteString(powershellArchVars(cfg.ExpectedArch))
	b.WriteString("\n")

	b.WriteString(powershellCheckAdmin())
	b.WriteString(powershellCheckArch())

	b.WriteString("if ($PayloadDir -eq '') { $PayloadDir = $PSScriptRoot }\n")
	if cfg.RequireAdmin {
		b.WriteString("CheckAdmin\n")
	}
	b.WriteString("CheckArch\n")
	b.WriteString("\n")
	b.WriteString("Push-Location $PayloadDir\n")
	b.WriteString("try {\n")
	if cfg.HasPayload {
		b.WriteString("    Expand-Archive -Path (Join-Path $PayloadDir 'payload.zip') -DestinationPath $PayloadDir -Force\n")
	}
	fmt.Fprintf(&b, "    Write-Host 'Running %%d command(s) for %%s %%s'\n", len(cfg.Commands), cfg.Name, cfg.Version)
	for _, cmd := range cfg.Commands {
		b.WriteString(runnerCommandBlock(cmd))
	}
	fmt.Fprintf(&b, "    Write-Host 'Completed %%d command(s)'\n", len(cfg.Commands))
	b.WriteString("} finally {\n")
	b.WriteString("    Pop-Location\n")
	b.WriteString("}\n")
	b.WriteString("Read-Host 'Press Enter to exit'\n")

	return b.String(), nil
}

// runnerCommandBlock renders one command's log line, invocation, and fail-fast check.
func runnerCommandBlock(cmd CommandSpec) string {
	var b strings.Builder
	if cmd.Exec != "" {
		// Log: Write-Host 'Running:' '<exec>' '<arg>' ...  (all single-quoted via psQuote)
		b.WriteString("    Write-Host 'Running:'")
		b.WriteString(" " + psQuote(cmd.Exec))
		for _, a := range cmd.Args {
			b.WriteString(" " + psQuote(a))
		}
		b.WriteString("\n")

		b.WriteString("    & (Join-Path $PayloadDir " + psQuote(cmd.Exec) + ")")
		for _, a := range cmd.Args {
			b.WriteString(" " + psQuote(a))
		}
		b.WriteString("\n")
		b.WriteString("    if ($LASTEXITCODE -ne 0) { Write-Host ('Command failed with exit code ' + $LASTEXITCODE); Read-Host 'Press Enter to exit'; exit 1 }\n")
	} else {
		b.WriteString("    Write-Host 'Running:' " + psQuote(cmd.Shell) + "\n")
		// Reset so a cmdlet-only shell (where no native sets $LASTEXITCODE) does not
		// trip the -ne 0 check on a stale/$null value.
		b.WriteString("    $LASTEXITCODE = 0\n")
		b.WriteString("    Invoke-Expression " + psQuote(cmd.Shell) + "\n")
		b.WriteString("    if ((-not $?) -or ($LASTEXITCODE -ne 0)) { Write-Host 'Command failed'; Read-Host 'Press Enter to exit'; exit 1 }\n")
	}
	return b.String()
}
```

- [ ] **Step 4: Run the runner tests to verify they pass**

Run: `cd cmd/onepkg && go test ./installer/ -run TestGeneratePowerShellRunner -v`
Expected: PASS.

- [ ] **Step 5: Run the full suite**

Run: `make test`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/onepkg/installer/powershell_runner.go cmd/onepkg/installer/powershell_runner_test.go
git commit -m "feat(windows-run): add GeneratePowerShellRunner for run.ps1"
```

---

### Task 6: main.go — non-Windows guard, mode branch, and `BuildForWindowsRun`

**Files:**
- Modify: `cmd/onepkg/main.go` (`BuildForPlatform` ~line 192; `BuildForWindows` ~line 324)
- Test: `cmd/onepkg/main_test.go`

**Interfaces:**
- Consumes (from Tasks 2,3,5): `cfg.Mode`, `cfg.Commands`, `cfg.RequireAdmin`; `installer.GeneratePowerShellRunner`, `installer.PowerShellRunnerConfig`, `installer.CommandSpec`; `BuildZipWithExtras`, `FileEntry`, `unameArch`, `platformOutputPath`.
- Produces: `BuildForWindowsRun(cfg *Config, platform string, entries []FileEntry, outputDir string) error`.

- [ ] **Step 1: Write the failing tests** (append to `cmd/onepkg/main_test.go`)

```go
func TestBuildForWindowsRunRejectsNonWindows(t *testing.T) {
	cfg := &Config{Name: "x", Version: "v1", Mode: "run"}
	err := BuildForPlatform(cfg, "linux-amd64", nil, nil, "")
	if err == nil {
		t.Fatal("BuildForPlatform should reject mode=run on non-windows platform")
	}
	if !strings.Contains(err.Error(), "Windows-only") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildForWindowsRunWithItems(t *testing.T) {
	projectDir := t.TempDir()
	binDir := filepath.Join(projectDir, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "runapp.exe"), []byte("fake-exe"), 0755)

	configContent := `
name = "runapp"
version = "v1.0.0"
mode = "run"
items = ["bin/runapp.exe"]

[[commands]]
exec = "bin/runapp.exe"
args = ["--do-stuff"]

[[commands]]
shell = "Write-Host shell-cmd"
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
	resolved, err := ResolveItems(projectDir, cfg.Items, platform)
	if err != nil {
		t.Fatalf("ResolveItems: %v", err)
	}
	entries, err := CollectFilesResolved(projectDir, resolved, cfg.Exclude)
	if err != nil {
		t.Fatalf("CollectFilesResolved: %v", err)
	}

	outputDir := filepath.Join(projectDir, "packages")
	os.MkdirAll(outputDir, 0755)

	if err := BuildForPlatform(cfg, platform, entries, nil, nil, outputDir); err != nil {
		t.Fatalf("BuildForPlatform: %v", err)
	}

	exeFile := filepath.Join(outputDir, "runapp_v1.0.0_windows_amd64.exe")
	data, err := os.ReadFile(exeFile)
	if err != nil {
		t.Fatalf("read exe: %v", err)
	}
	if data[0] != 'M' || data[1] != 'Z' {
		t.Error("exe does not start with MZ")
	}
	if !bytes.HasSuffix(data, []byte("ONEPKG")) {
		t.Error("exe does not end with ONEPKG magic")
	}
	// run.ps1 text is embedded verbatim in the exe bytes.
	if !bytes.Contains(data, []byte("& (Join-Path $PayloadDir 'bin/runapp.exe')")) {
		t.Error("exe missing exec command in embedded run.ps1")
	}
	if !bytes.Contains(data, []byte("Invoke-Expression 'Write-Host shell-cmd'")) {
		t.Error("exe missing shell command in embedded run.ps1")
	}
	if bytes.Contains(data, []byte("New-Service")) {
		t.Error("run exe should not contain service code")
	}
}

func TestBuildForWindowsRunShellOnly(t *testing.T) {
	projectDir := t.TempDir()
	configContent := `
name = "shellonly"
version = "v1.0.0"
mode = "run"

[[commands]]
shell = "Write-Host hello"
`
	configPath := filepath.Join(projectDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)

	cfg, err := ParseConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(projectDir); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	outputDir := filepath.Join(projectDir, "packages")
	os.MkdirAll(outputDir, 0755)

	// Shell-only: no items -> empty entries.
	if err := BuildForPlatform(cfg, "windows-amd64", nil, nil, nil, outputDir); err != nil {
		t.Fatalf("BuildForPlatform: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "shellonly_v1.0.0_windows_amd64.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("ONEPKG")) {
		t.Error("exe does not end with ONEPKG magic")
	}
	if bytes.Contains(data, []byte("Expand-Archive")) {
		t.Error("shell-only run exe should not contain Expand-Archive")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cmd/onepkg && go test -run 'TestBuildForWindowsRun' -v`
Expected: FAIL (`BuildForWindowsRun` undefined; guard absent).

- [ ] **Step 3: Add the non-Windows guard to `BuildForPlatform`**

In `cmd/onepkg/main.go`, in `BuildForPlatform`, immediately after `arch := parts[1]` (right after the `parts` parse at the top of the function), add:

```go
	if cfg.Mode == "run" && goos != "windows" {
		return fmt.Errorf("mode=\"run\" is Windows-only; got platform %q", platform)
	}
```

- [ ] **Step 4: Add the mode branch to `BuildForWindows`**

At the very top of `func BuildForWindows(...)` in `cmd/onepkg/main.go`, add:

```go
	if cfg.Mode == "run" {
		return BuildForWindowsRun(cfg, platform, entries, outputDir)
	}
```

- [ ] **Step 5: Implement `BuildForWindowsRun`**

Add this function to `cmd/onepkg/main.go` (e.g. right after `BuildForWindows`):

```go
// BuildForWindowsRun builds a mode="run" self-extracting Windows exe: a payload
// zip (no VERSION file), a lean run.ps1, wrapped via the existing exe-wrapper.
// The stub is unchanged; run.ps1 tolerates the -InstallDir/-Action it forwards.
func BuildForWindowsRun(cfg *Config, platform string, entries []FileEntry, outputDir string) error {
	parts := strings.SplitN(platform, "-", 2)
	arch := parts[1]

	// No VERSION file: run mode has no verify action. Empty zip when no entries.
	payloadZip, err := BuildZipWithExtras(entries, *cfg.PreservePermissions, nil)
	if err != nil {
		return fmt.Errorf("build zip: %w", err)
	}

	commands := make([]installer.CommandSpec, 0, len(cfg.Commands))
	for _, c := range cfg.Commands {
		commands = append(commands, installer.CommandSpec{
			Exec:  c.Exec,
			Args:  c.Args,
			Shell: c.Shell,
		})
	}

	ps1Content, err := installer.GeneratePowerShellRunner(installer.PowerShellRunnerConfig{
		Name:         cfg.Name,
		Version:      cfg.Version,
		Platform:     platform,
		ExpectedArch: unameArch("windows", arch),
		RequireAdmin: cfg.RequireAdmin,
		HasPayload:   len(entries) > 0,
		Commands:     commands,
	})
	if err != nil {
		return fmt.Errorf("generate powershell runner: %w", err)
	}

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

- [ ] **Step 6: Run the build tests to verify they pass**

Run: `cd cmd/onepkg && go test -run 'TestBuildForWindowsRun|TestBuildForWindows' -v`
Expected: PASS (both new run tests and the existing install `TestBuildForWindows` pass — the stub is untouched).

- [ ] **Step 7: Run the full suite**

Run: `make test`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add cmd/onepkg/main.go cmd/onepkg/main_test.go
git commit -m "feat(windows-run): build self-extracting run-mode exe (stub unchanged)"
```

---

### Task 7: Docs — config example + CLAUDE.md

**Files:**
- Modify: `docs/config-example.toml`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add a run-mode example to `docs/config-example.toml`**

Append a commented run-mode example (keep any existing install example intact):

```toml

# --- mode = "run": one-shot command/script package (Windows-only) ---
# Double-clicking the .exe runs the commands once in order, then exits.
# No install/uninstall, no service, no permanent files. Items are optional
# (omit them for shell-only packages).
#
# name = "cleanup-bundle"
# version = "v1.0.0"
# mode = "run"
# require_admin = true
#
# items = [
#   "bin/cleanup.{platform}",
#   "scripts/setup.ps1",
# ]
#
# [[commands]]
# exec = "bin/cleanup"               # bundled file, resolved against the temp dir
# args = ["--all", "--force"]
#
# [[commands]]
# shell = "netsh advfirewall set allprofiles state off"   # arbitrary PowerShell
#
# [[commands]]
# exec = "scripts/setup.ps1"
# args = ["-Mode", "quick"]
```

- [ ] **Step 2: Document `mode = "run"` in `CLAUDE.md`**

Under **Key Conventions**, add a bullet:

```markdown
- **Run mode (`mode = "run"`):** Windows-only one-shot package. `[[commands]]` entries (each exactly one of `exec`+`args` for a bundled file, or `shell` for arbitrary PowerShell) run once in order; no install/service/`VERSION`. `require_admin` gates (does not elevate). `items` optional (shell-only packages omit them). Install mode rejects `[[commands]]`; run mode rejects `services`/`dirs`/`system_overlay`. Run packages reuse the **unchanged** exe stub — `run.ps1` declares and ignores the `-InstallDir`/`-Action` the stub forwards.
```

- [ ] **Step 3: Build and run a quick sanity check**

Run: `make build`
Expected: builds `cmd/onepkg/onepkg` with no errors (stubs already generated).

- [ ] **Step 4: Commit**

```bash
git add docs/config-example.toml CLAUDE.md
git commit -m "docs: document mode=\"run\" and [[commands]]"
```

---

## Self-Review Notes

- **Spec coverage:** Config schema (Task 2), validation rules incl. Windows-only (Tasks 3 + 6 guard), build flow / `BuildForWindowsRun` (Task 6), stub-unchanged approach (Tasks 4–6 — no stub rebuild), `run.ps1` runtime behavior incl. params/admin/arch/conditional-extract/commands/fail-fast/pause (Task 5), shared-helper refactor (Task 4), testing strategy (Tasks 2/3/5/6), files-to-modify (all tasks), docs (Task 7). All spec sections map to a task.
- **Placeholder scan:** none — every code/test step contains real Go or PowerShell.
- **Type consistency:** `CommandConfig` (config, Task 2) → mapped to `CommandSpec` (installer, Task 5) in `BuildForWindowsRun` (Task 6). `PowerShellRunnerConfig`/`CommandSpec` field names match across Task 5 test and Task 6 caller. `itemMatches` signature matches its use in `validateRun`.
- **Divergence from spec, flagged:** the spec's literal shell check `-not $? -or $LASTEXITCODE -ne 0` would false-fail on a cmdlet-only shell (the `$null`/stale-`$LASTEXITCODE` case). Task 5 keeps that check but precedes it with `$LASTEXITCODE = 0`, which makes it correct while honoring the spec's intent. (If desired, the spec's step 5 can be updated to mention the reset; behavior is as the spec intended.)
