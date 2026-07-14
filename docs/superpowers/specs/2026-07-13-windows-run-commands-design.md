# Windows Run-Commands Package — Design Spec

**Date:** 2026-07-13 (revised 2026-07-14)
**Status:** Approved; revised after design review
**Scope:** Add a Windows-only package mode that runs a list of commands (bundled executables and/or arbitrary PowerShell) one-shot, without installing files.

> **Revision 2026-07-14 (design review).** The original "stub simplification + rebuild" approach was based on a misreading of the stub and would have silently broken `myapp.exe uninstall|version|list|verify` and custom `-InstallDir`. The stub is now left **unchanged**; `run.ps1` instead declares (and ignores) the params the stub forwards. Other fixes: factor `CheckAdmin`/`CheckArch` into shared helpers (and emit the arch variables they read), reject non-Windows platforms in run mode, tighten command-`exec` matching, and harden `shell` failure detection.

## Motivation

onepkg currently produces Windows packages that *install*: extract a payload to a permanent directory and optionally register a Windows Service. Some use cases need neither — they need a double-clickable `.exe` that runs a sequence of commands (a bundled diagnostic tool with args, a `netsh`/`sc`/registry one-liner, a bundled setup script) and leaves nothing installed. This spec adds a `mode = "run"` package type for that.

## Requirements

- **Windows-only.** Targets Windows 8/10/11, `windows-amd64` and `windows-arm64`.
- **One-shot run.** Executing the `.exe` runs all defined commands once, in order, then exits. No install/uninstall lifecycle.
- **Mixed command sources.** Commands may be bundled executables (run from a temp-extracted payload) or arbitrary PowerShell expressions, or a mix.
- **No installation.** No files written to a permanent location; no service registration. Temp artifacts are cleaned up after the run.
- **Reuse the exe-wrapper architecture.** Same stub + ps1 + zip + manifest + magic-marker format as install packages.

## Non-Goals

- Install + run-commands hybrid (mutually exclusive with `mode = "install"`).
- Named actions / menu-driven command selection.
- An "undo" command set.
- Linux/macOS run-commands mode (Windows-only for now).
- Running commands as a specific user (admin elevation is the only privilege control).

## Config Schema

A new top-level `mode` field selects the package type. `mode = "install"` (the default) is today's behavior, unchanged. `mode = "run"` enables the run-commands package.

```toml
name = "cleanup-bundle"
version = "v1.0.0"
mode = "run"                       # "install" (default) | "run"

# items: OPTIONAL in run mode (payload for bundled exec commands).
# Omit entirely if all commands are `shell`-only.
items = [
  "bin/cleanup.{platform}",        # platform-specific binary, packed as bin/cleanup
  "scripts/setup.ps1",             # bundled script
]

# Commands run once, in order, when the .exe is executed.
# Each entry is exactly one of: exec+args (bundled file) OR shell (PowerShell).
[[commands]]
exec = "bin/cleanup"               # bundled file, resolved against the temp extraction dir
args = ["--all", "--force"]

[[commands]]
shell = "netsh advfirewall set allprofiles state off"   # arbitrary PowerShell expression

[[commands]]
exec = "scripts/setup.ps1"         # a bundled .ps1
args = ["-Mode", "quick"]

# Optional: require admin elevation for the whole run (default: false)
require_admin = true
```

### Config struct additions (`config.go`)

```go
type Config struct {
    // ...existing fields...
    Mode         string          `toml:"mode"`          // "install" (default) | "run"
    Commands     []CommandConfig `toml:"commands"`
    RequireAdmin bool            `toml:"require_admin"` // run mode only; default false (zero value)
}

type CommandConfig struct {
    Exec  string   `toml:"exec"`   // bundled file path; empty if Shell set
    Args  []string `toml:"args"`   // args for Exec
    Shell string   `toml:"shell"`  // arbitrary PowerShell expression; empty if Exec set
}
```

`RequireAdmin` is a plain `bool` — zero value `false` is the default, so no pointer type is needed (unlike `PreservePermissions`/`RecoveryDelayMs`, which need pointers because their defaults are non-zero). `Mode` defaults to `"install"` in `ApplyDefaults`.

### Validation rules

- `mode = "run"` is **explicit**, not inferred from `[[commands]]` presence. Inferring would produce confusing errors when an install config gains commands and silently switches type.
- Run mode:
  - At least one `[[commands]]` entry required.
  - Each command must have exactly one of `exec` or `shell` (not both, not neither).
  - `exec` must reference a bundled item via **exact match or stripped `{platform}` form** (e.g. `bin/cleanup` matches item `bin/cleanup.{platform}`). A `{platform}` marker in a command's `exec` is **rejected** (require the stripped form, as service exec does). Fail fast if not found. Factor a shared `itemMatches(items, exec)` helper using exact-or-stripped matching only — **not** service-exec's looser prefix match (`config.go:185`).
  - `items` optional (empty allowed when all commands are `shell`-only).
  - `services`, `dirs`, `system_overlay` **rejected** with a clear error (run packages don't install).
- Install mode: `[[commands]]` **rejected** (mutual exclusivity).

## Architecture & Build Flow

### Build flow (`main.go`)

`BuildForPlatform` still routes `case "windows"` to `BuildForWindows`. At the top of `BuildForWindows`, branch on `cfg.Mode`:

```go
func BuildForWindows(cfg *Config, platform string, entries []FileEntry, shaMap map[string]string, outputDir string) error {
    if cfg.Mode == "run" {
        return BuildForWindowsRun(cfg, platform, entries, outputDir)
    }
    // ...existing install path unchanged...
}
```

`BuildForWindowsRun`:
1. Build ZIP from `entries`. **No `VERSION` file** — run mode has no verify action. If `entries` is empty (shell-only package), pass `nil`/empty zip bytes.
2. Generate `run.ps1` via new `installer.GeneratePowerShellRunner(...)`, passing `HasPayload = len(entries) > 0`.
3. Generate exe via existing `installer.GenerateExeWrapper(...)` — same stub + ps1 + zip + manifest + magic format, unchanged.
4. Write `.exe` to output (existing `platformOutputPath` already returns `.exe` for windows).

**Run mode is Windows-only.** Add a guard at the top of `BuildForPlatform` (or in `main`'s per-platform loop): if `cfg.Mode == "run"` and the platform's OS is not `windows`, return an error. Without it, `mode = "run"` with e.g. `--platform linux-arm64` silently falls through `BuildForPlatform`'s linux branch and emits a normal install `.sh` that ignores `[[commands]]`.

### Stub — unchanged (no rebuild)

The stub is **not modified**. It already works for both install and run packages because it is generic: it reads the manifest, spills the embedded `.ps1` (always) and `payload.zip` (only for the default `install` action) to a temp dir, then invokes `powershell -File <ps1> -PayloadDir <tmp> -InstallDir <dir> -Action <action>` (`stub-src/wrapper.go:105-108`).

The one compatibility concern: PowerShell errors on a named parameter a `-File` script doesn't declare. `install.ps1` declares `$PayloadDir`, `$InstallDir`, and `$Action`; `run.ps1` declares only `$PayloadDir`. So that the **unchanged** stub can drive `run.ps1`, the runner declares and **ignores** the two extra params (see the param block in the next section). This preserves every existing install behavior — `myapp.exe uninstall|version|list|verify` and custom `-InstallDir` all keep working — and avoids rebuilding the PE stubs, which can't be executed or validated on macOS.

Note on the stub's `if action == "install"` zip-write gate (`wrapper.go:89-95`): a double-clicked run package has no args, so `parseStubArgs` defaults `action` to `"install"` and `payload.zip` is written for `run.ps1` to extract. Bare keywords (e.g. `runpkg.exe version`) would skip the write, but action keywords are meaningless for run packages, so this is acceptable.

### Manifest — unchanged

No `mode` field is added to the manifest. The stub is generic — it reads the manifest's sizes and runs the embedded `.ps1`, treating `install.ps1` and `run.ps1` identically — so the manifest remains `{name, version, ps1_size, zip_size}`. The ps1 content itself determines install vs. run behavior.

## `run.ps1` Runtime Behavior

A new `installer.GeneratePowerShellRunner(cfg PowerShellRunnerConfig) (string, error)` produces a lean `run.ps1`. Responsibilities, in order:

1. **Param block** — declare all three params the stub forwards; ignore two:
   ```powershell
   param(
       [string]$PayloadDir = '',
       [string]$InstallDir = '',    # accepted for stub parity; ignored in run mode
       [string]$Action = 'install'  # accepted for stub parity; ignored in run mode
   )
   ```
   If `$PayloadDir` is empty, default to `$PSScriptRoot` (same pattern as `install.ps1`) so the script is also runnable standalone. **Why:** the unchanged stub passes `-InstallDir`/`-Action`; PowerShell errors on a named param a `-File` script doesn't declare, so the runner must declare them. They are unused in run mode.
2. **Admin check (conditional):** if `RequireAdmin`, emit `CheckAdmin`; otherwise omit. Note `require_admin` is a *check*, not elevation — a non-elevated run prints an error and `exit 1`; it does **not** trigger a UAC prompt (same semantics as `install.ps1`).
3. **Arch check (always) + arch variables:** emit `$EXPECTED_ARCH` and `$ALLOWED_ARCHS` (the latter includes `ARM64` when `ExpectedArch == "AMD64"`, mirroring `install.ps1`'s emulation logic at `powershell.go:55-61`), then `CheckArch`. The package is platform-specific (amd64 vs arm64). Without these variables `CheckArch` reads `$null` and rejects every host.
4. **Extract payload (conditional):** emit extraction code **only if `HasPayload`**:
   ```powershell
   Expand-Archive -Path (Join-Path $PayloadDir 'payload.zip') -DestinationPath $PayloadDir -Force
   ```
   Bundled files then live under `$PayloadDir` at their packed paths. For shell-only packages, skip extraction entirely — no empty-zip guard needed.
5. **Run commands sequentially** in config order. Set working dir to `$PayloadDir` (`Push-Location`/`Pop-Location`) so relative paths in `shell` commands resolve against the extracted payload:
   - `exec` command → `$exe = Join-Path $PayloadDir $exec; & $exe @args`. Failure if `$LASTEXITCODE -ne 0`.
   - `shell` command → `Invoke-Expression $shell` (PowerShell expression; users wanting cmd syntax write `cmd /c ...`). Failure if `-not $? -or $LASTEXITCODE -ne 0` — the `$LASTEXITCODE` arm catches a failing native exe inside the expression, which `$?` alone can miss.
   - **Fail-fast:** on first failure, print which command failed and its exit code, then `exit 1`. No continue.
6. **Logging + keep window open:** print each command before running (`Running: bin/cleanup --all --force` or `Running: <shell>`), stream its output, print a final `Completed N command(s)` summary on success. Because the stub is `-H windowsgui`, a double-clicked run spawns a child `powershell.exe` console that closes on exit — so end the script with `Read-Host "Press Enter to exit"` on success (and just before `exit 1` on failure) so a double-clicker can read the output. (A future `quiet`/`no_pause` option can skip this.)
7. **Cleanup:** none in `run.ps1`. The stub's existing `defer os.RemoveAll(tmpDir)` wipes extracted files, `payload.zip`, and `run.ps1` after the PowerShell process exits.

### `PowerShellRunnerConfig`

```go
type PowerShellRunnerConfig struct {
    Name          string
    Version       string
    Platform      string
    ExpectedArch  string // AMD64 or ARM64
    RequireAdmin  bool
    HasPayload    bool              // emit extraction block iff true
    Commands      []CommandSpec
}

type CommandSpec struct {
    Exec  string   // non-empty → run bundled file
    Args  []string
    Shell string   // non-empty → run PowerShell expression
}
```

### Why conditional extraction

A shell-only package has no `payload.zip` content (zip bytes are empty/nil). Emitting extraction code only when `HasPayload` is true avoids `Expand-Archive` errors on empty archives and keeps shell-only packages minimal — mirroring how `GeneratePowerShellInstaller` conditionally emits service functions.

### Shared PowerShell helpers (refactor)

`CheckAdmin`, `CheckArch`, and the arch-variable emission (`$EXPECTED_ARCH`/`$ALLOWED_ARCHS`) currently live **inside** `GeneratePowerShellInstaller`'s monolithic `powershellFunctions` raw-string blob, alongside install-only functions (`CreateDirs`, `ApplyOverlays`, `Install`, …). Extract them into small shared builders — e.g. `powershellCheckAdmin()`, `powershellCheckArch()`, `powershellArchVars(expectedArch)` — and call them from **both** the installer and the new runner. This avoids duplicating the check code in `run.ps1` (where it would drift) and is the only sensible way to "emit the existing `CheckAdmin`/`CheckArch`" that this spec relies on. Also parametrize `CheckAdmin`'s elevation hint (it currently hardcodes `-Action install`, `powershell.go:123`) so the run variant doesn't print a misleading install-only message.

## Testing Strategy

TDD throughout. We can't execute Windows `.exe`s on macOS, so tests verify generated artifact structure and config logic — the same approach as existing Windows support.

### Config (`config_test.go`)
- `ParseConfig` reads `mode = "run"` + `[[commands]]` into the struct correctly.
- `ApplyDefaults` sets `Mode = "install"` when omitted.
- `Validate` run mode: requires ≥1 command; each command has exactly one of `exec`/`shell`; `exec` must match a bundled item (exact or stripped `{platform}` form); rejects `services`/`dirs`/`system_overlay`; allows empty `items`.
- `Validate` install mode: rejects `[[commands]]`.

### Runner (`installer/powershell_runner_test.go`, new file)
- `GeneratePowerShellRunner` output declares the `$PayloadDir`, `$InstallDir`, and `$Action` params (so the unchanged stub's invocation doesn't hit PowerShell's "parameter cannot be found" error).
- Contains `$EXPECTED_ARCH`/`$ALLOWED_ARCHS` and `CheckArch` always; contains `CheckAdmin` iff `RequireAdmin`; contains `Expand-Archive` iff `HasPayload`; omits `Expand-Archive` when `HasPayload = false`.
- `exec` command rendered as `& (Join-Path $PayloadDir 'bin/cleanup') --all --force` (args quoted via `psQuote`).
- `shell` command rendered via `Invoke-Expression`, with a failure check covering both `$?` and `$LASTEXITCODE`.
- Fail-fast: each command followed by an exit-code check that `exit 1`s on failure.
- Ends with a `Read-Host` pause so double-click output stays visible.
- No install/uninstall/service/version/list/verify code present (lean output).

### Build (`main_test.go` or a build test)
- `BuildForWindowsRun` with items → produces a valid `.exe` (MZ header + `ONEPKG` magic marker), manifest round-trips (`ps1_size`/`zip_size` correct), and the embedded `run.ps1` contains the commands.
- `BuildForWindowsRun` with no items (shell-only) → still a valid `.exe`; embedded `run.ps1` has no `Expand-Archive`.
- `mode = "run"` with a non-Windows platform returns an error (the guard added to `BuildForPlatform`).
- Install path unchanged: existing `TestBuildForWindows` still passes — the stub is **not** modified, so nothing about install packages changes.

### Stub unchanged — no rebuild verification needed
The stub is not rebuilt, so existing PE-validity coverage (`TestBuildForWindows`) still applies unchanged. The behavior-preservation proof is now structural rather than empirical: because the stub is untouched, every install feature (`uninstall`/`version`/`list`/`verify`, custom `-InstallDir`) is unaffected by definition. The only new coupling to assert is that `run.ps1` declares the params the stub forwards (covered by the runner test above).

### Out of scope (can't test on macOS)
Actual command execution on Windows, admin elevation prompts, `Expand-Archive` runtime behavior, temp-dir cleanup after background processes. Same gap as existing Windows support — documented, deferred to a Windows CI runner.

## Files to Create / Modify

| File | Change |
|------|--------|
| `cmd/onepkg/config.go` | Add `Mode`, `Commands`, `RequireAdmin` fields, `CommandConfig` struct; `ApplyDefaults` default for `Mode`; `Validate` branching. |
| `cmd/onepkg/config_test.go` | Tests for run-mode parsing, defaults, validation. |
| `cmd/onepkg/main.go` | Add `mode = "run"` non-Windows guard to `BuildForPlatform`; `BuildForWindows` branches to `BuildForWindowsRun`; new `BuildForWindowsRun` function. |
| `cmd/onepkg/main_test.go` (or build test) | Tests for `BuildForWindowsRun` (with/without items) and the non-Windows guard. |
| `cmd/onepkg/installer/powershell.go` | Refactor: extract `CheckAdmin`, `CheckArch`, and arch-variable emission into shared builders used by both installer and runner; parametrize `CheckAdmin`'s elevation hint. |
| `cmd/onepkg/installer/powershell_runner.go` (new) | `GeneratePowerShellRunner`, `PowerShellRunnerConfig`, `CommandSpec`. |
| `cmd/onepkg/installer/powershell_runner_test.go` (new) | Tests for runner generation. |
| `docs/config-example.toml` | Add `mode = "run"` example with `[[commands]]`. |
| `CLAUDE.md` | Document `mode = "run"` and `[[commands]]`. |
