# Cross-Platform Run Mode & Per-Platform Field Overrides — Design Spec

**Date:** 2026-07-15
**Status:** Approved
**Builds on:** `2026-07-13-windows-run-commands-design.md` (run mode), `2026-07-14-config-marker-namespaces-design.md` (`{dst.NAME}`).
**Scope:** (1) Extend `mode = "run"` from Windows-only to Linux and macOS; (2) add per-platform field overrides so one config can target multiple platforms; (3) add one cross-platform `{dst.HOME}` marker. Together these let a single `config.toml` build run-mode packages for Linux, macOS, and Windows, and let install/run paths and commands vary per OS without OS-specific config files.

## Motivation

Run mode today is Windows-only (`main.go` guard rejects `mode="run"` on non-Windows). A run-commands package is just as useful on Linux/macOS — a self-extracting script that runs a one-shot sequence of commands and leaves nothing installed. Linux needs the terminal-output form (no GUI dialog); macOS wants a results dialog for parity with the Windows `MessageBox`.

Run-mode commands are **inherently platform-specific**: a `shell` command is PowerShell on Windows but bash on Linux/macOS, the bundled `exec` binary differs, and even args differ (`--all` vs `/all`). The existing flat `[[commands]]` only works for a single target platform. Rather than invent a curated per-OS marker vocabulary (considered and rejected during design — it cannot express different binaries, arg shapes, or shell languages), this spec adds **per-platform field overrides** via a uniform `<field>_<os>` suffix convention. One override mechanism covers both commands and paths, and lets one config build for several platforms.

A small, high-value exception is kept as a real marker: `{dst.HOME}`, the target user's home directory, resolving on **all three** platforms. It is the one destination variable that is genuinely cross-platform and worth a name rather than a literal.

## Requirements

- **Run mode on Linux & macOS.** `mode = "run"` builds a self-extracting `.sh` for `linux-*` and `darwin-*` targets (Windows `.exe` behavior unchanged).
- **Linux run-mode: no dialog.** Prints per-command `[OK]`/`[FAIL]` and a tally to stdout; exits `0` (all succeeded) or `1` (any failed). No GUI.
- **macOS run-mode: results dialog.** Same stdout behavior, **plus** an `osascript` dialog at the end summarizing results (parity with the Windows `MessageBox`).
- **Run-all semantics.** Commands continue past failures (mirrors the Windows runner); every command's outcome is recorded.
- **Per-platform field overrides.** A base field plus optional `_<os>` variants; the variant wins for that OS, else the base applies. Covers `commands[].exec|args|shell`, `dirs[].path`, `system_overlay[].system`.
- **`{dst.HOME}` cross-platform marker.** Resolves to `$HOME` on Linux, macOS, and Windows at target time. Allowed everywhere `{dst.*}` is allowed, on every platform.
- **Fail fast.** A platform with no resolved command set, an exec that doesn't match an item, a Windows-only `{dst.*}` marker on a non-Windows resolved path, etc., are clear build-time errors.
- **No regression.** Unmarked, single-platform configs produce byte-identical output to today.

## Non-Goals

- A curated Unix `{dst.*}` vocabulary beyond `{dst.HOME}` (e.g. `{dst.LinuxData}`, `{dst.MacAppSupport}`). Per-platform overrides subsume this; authors write literal paths.
- XDG env-var honoring (`$XDG_DATA_HOME`, etc.). Authors write fixed paths; `$HOME` is the only runtime-resolved variable.
- Different command *counts* per platform from one `[[commands]]` list (every `[[commands]]` entry applies to all platforms, resolved per field). If genuinely different command sets per OS are needed later, a section-level `commands_<os>` can be added; YAGNI now.
- `require_admin` semantics on Unix run-mode (stays Windows-admin; ignored on Unix this iteration — commands needing root fail on their own).
- A double-clickable `.app` bundle for macOS. The macOS run package is a `.sh` that ends with an `osascript` dialog, same artifact shape as today's macOS install `.sh`.

## Per-Platform Field Overrides

### Convention

For every platform-sensitive field, the base key is the value used by any platform that has no specific override; a `_<os>` sibling key overrides it for that OS family (`linux`, `macos`, `windows`). `_<os>` is keyed by **OS family**, not `{os}-{arch}` — arch never affects these paths.

**Resolution rule (one rule, everywhere):** for target goos `g`, use `<field>_<g>` if it is set (non-empty string, or non-empty slice), else `<field>`.

| Config location | Base key | Override keys |
|---|---|---|
| `commands[].exec` | `exec` | `exec_linux` / `exec_macos` / `exec_windows` |
| `commands[].args` | `args` | `args_linux` / `args_macos` / `args_windows` |
| `commands[].shell` | `shell` | `shell_linux` / `shell_macos` / `shell_windows` |
| `dirs[].path` | `path` | `path_linux` / `path_macos` / `path_windows` |
| `system_overlay[].system` | `system` | `system_linux` / `system_macos` / `system_windows` |

### Example

```toml
name = "cleanup"
version = "1.0.0"
mode = "run"
items = ["bin/cleanup.{platform}", "payload.conf"]

# One command list; each field varies per OS where needed.
[[commands]]
exec         = "bin/cleanup"            # all platforms (.exe auto-appended on Windows; exec stays extensionless)
args         = ["--all"]
args_windows = ["/all"]
shell_linux  = "mkdir -p \"$HOME/.local/share/cleanup\" && cp payload.conf \"$HOME/.local/share/cleanup/\""
shell_macos  = "mkdir -p \"$HOME/Library/Application Support/cleanup\""
shell_windows= "New-Item -ItemType Directory -Force \"$env:APPDATA\\cleanup\" | Out-Null"
```

`exec` stays extensionless: on Windows the `.exe` is auto-appended to the bundled item (see CLAUDE.md "Windows executable extension"), so `exec_windows` is only needed when the path *itself* differs between platforms — not for the extension. A literal `exec_windows = "bin/cleanup.exe"` against a `{platform}` item is rejected at build time, because it matches no item (the item's stripped form is `bin/cleanup`, not `bin/cleanup.exe`).

Install-mode path overrides (shared owner/mode/group; only the path differs):

```toml
[[dirs]]
owner = "root"
mode  = "0755"
path         = "/var/lib/myapp"                     # default
path_macos   = "/Library/Application Support/myapp"
path_windows = "{dst.CommonAppDataFolder}\\myapp"   # Windows marker still usable in path_windows
```

### Config struct additions (`config.go`)

```go
type CommandConfig struct {
    Exec  string   `toml:"exec"`
    Args  []string `toml:"args"`
    Shell string   `toml:"shell"`
    // per-OS overrides
    ExecLinux    string   `toml:"exec_linux"`
    ExecMacos    string   `toml:"exec_macos"`
    ExecWindows  string   `toml:"exec_windows"`
    ArgsLinux    []string `toml:"args_linux"`
    ArgsMacos    []string `toml:"args_macos"`
    ArgsWindows  []string `toml:"args_windows"`
    ShellLinux   string   `toml:"shell_linux"`
    ShellMacos   string   `toml:"shell_macos"`
    ShellWindows string   `toml:"shell_windows"`
}

// resolveFor collapses the base + overrides into the effective values for one
// target OS. Command type (exec vs shell) is fixed by the base field; `_<os>`
// overrides refine the same-typed value per platform. The base exactly-one-of
// rule stays in `validateRun`; build-time checks verify the resolved exec names an item.
func (c CommandConfig) resolveFor(goos string) CommandConfig { ... }
```

`DirConfig` gains `PathLinux/PathMacos/PathWindows`; `OverlayConfig` gains `SystemLinux/SystemMacos/SystemWindows`, each with an analogous resolver. A shared helper picks string/slice-or-default by goos.

### `{dst.HOME}` — the one cross-platform marker

`{dst.HOME}` resolves to the target user's home directory. It is substituted to the **literal token `$HOME`** at build time, on all platforms:

- **Linux / macOS** (bash): `$HOME` is a standard environment variable.
- **Windows** (PowerShell): `$HOME` is an automatic variable equal to the user profile directory.

Because `$HOME` is valid in both bash and PowerShell, one substitution rule covers every platform; no runtime definition line is emitted (unlike `{dst.AppDataFolder}`, which defines `$AppDataFolder` via `[Environment]::GetFolderPath`). Concretely:

- Add `HOME` as a cross-platform marker distinct from the Windows-only `KnownFolders` set. `installer.ValidateKnownFolderMarkers` and `installer.HasKnownFolderMarker` accept it; `EmitPathExpr`/`ReplaceMarkers` render `{dst.HOME}` as `$HOME` (no definition); `EmitKnownFolderDefs` skips `HOME`.
- For Unix targets, a build-time pass substitutes `{dst.HOME}` → `$HOME` in resolved path/shell/args strings before the `.sh` is emitted (the shell expands `$HOME` at runtime).
- The build-time guard (below) permits `{dst.HOME}` on non-Windows targets.

Example: `shell_linux = "mkdir -p \"{dst.HOME}/.config/myapp\""` → emitted as `mkdir -p "$HOME/.config/myapp"`.

## Validation Rules

- **Override fields are plain config.** They parse and default like any field; an override may be empty (falls back to the base).
- **Stray `{dst.*}` allow-list (updated).** `{dst.*}` markers (including `{dst.HOME}`) remain valid only in: `dirs[].path` and its `_<os>` variants, `system_overlay[].system` and variants, run `commands[].args` and variants, run `commands[].shell` and variants. The `dstSupportedFieldRe` regex in `markers.go` is extended to match the new `_<os>` field paths (e.g. `Commands[\d+]\.ShellLinux`, `Dirs[\d+]\.PathMacos`).
- **Per-platform checks move to build time.** `Validate()` (no platform context) enforces structural rules on all present override fields and that run mode has at least one command somewhere (base or any override). The per-target checks happen in `BuildForPlatform`, which knows goos:
  - The **resolved** command set for the target OS is non-empty.
  - Each resolved command has **exactly one** of exec/shell.
  - A resolved `exec` matches a bundled item (exact or stripped-`{platform}` form; `.exe` auto-append on Windows unchanged).
  - A resolved `dirs[].path` / `overlay.system` is non-empty.
- **Install mode still rejects `[[commands]]`** (any form). Run mode still rejects `services`/`dirs`/`system_overlay`.

## Build-Time Guard (refined)

Today (`main.go:214-225`) any `{dst.*}` marker in `dirs[].path` / `overlay.system` is rejected for non-Windows targets. That is refined to operate on the **resolved path for the target OS**, and to let `{dst.HOME}` through:

- Build for goos `g`: take each dir/overlay's resolved path (`path_<g>` or `path`). If that resolved path contains a `{dst.X}` marker where `X != HOME`, fail fast (Windows-only marker on a non-Windows target). `{dst.HOME}` is allowed.
- A `{dst.AppDataFolder}` sitting in `path_windows` is **not** an error for a Linux build — that field is unused for Linux. Only the resolved path is checked.
- Windows builds are unchanged: `{dst.*}` (including `{dst.HOME}`) resolve via the existing PowerShell mechanism.

## Architecture & Build Flow

### `main.go`

- **Lift the Windows-only run-mode guard** (`main.go:208-210`). `mode="run"` is now permitted for `linux` and `darwin`. (Remove the `goos != "windows"` rejection; keep a sensible default if `mode` is invalid.)
- **Per-platform resolution before build.** For each target platform, resolve `CommandConfig`/`DirConfig`/`OverlayConfig` via their `resolveFor(goos)` so the platform-specific generators receive already-collapsed values. (Generators stay single-platform; the override collapse is a config-layer concern.)
- **Route Unix run mode.** In `BuildForPlatform`, for `linux`/`darwin` when `cfg.Mode == "run"`, call a new `BuildForUnixRun(cfg, platform, entries, outputDir)`. Install mode continues through the existing `.sh` path unchanged. Windows run mode (`BuildForWindowsRun`) is unchanged.
- The existing non-Windows tar.gz build (`BuildTarGZ`) is reused for the Unix run payload. Run mode embeds **no `VERSION`** file (no verify action), identical to Windows run mode.

### `BuildForUnixRun`

1. Build tar.gz from `entries` (no `VERSION`). Empty/nil when `items` is empty (shell-only package).
2. Generate the run-mode `.sh` via new `installer.GenerateShellRunner(ShellRunnerConfig)` with `HasPayload = len(entries) > 0` and `Goos` (`linux` or `darwin`) to control the dialog.
3. Resolve each command for `goos`; substitute `{dst.HOME}` → `$HOME` in resolved strings.
4. Write the `.sh` via the existing `platformOutputPath` (already returns `.sh` for non-Windows).

### `installer.GenerateShellRunner` — new

A lean run-mode `.sh`, paralleling `GeneratePowerShellRunner`. Responsibilities, in order:

1. `#!/bin/bash`, header, `set -uo pipefail` (**not** `-e` — run-all continues past failures).
2. PKG/`EXPECTED_ARCH` vars; the `PAYLOAD` base64 blob.
3. Reuse the existing `check_arch` (factor it out of `shell.go`'s embedded blob into a shared snippet so install and run `.sh` stay in sync).
4. `TMPDIR=$(mktemp -d)`; `trap 'rm -rf "$TMPDIR"' EXIT`.
5. `check_arch`.
6. Conditional payload extraction (only if `HasPayload`): `printf '%s' "$PAYLOAD" | base64 -d | tar xzf - -C "$TMPDIR"`; then `cd "$TMPDIR"`.
7. Run resolved commands in order, recording results:
   - `exec` → `"$TMPDIR/<exec>" <args...>`; failure if exit code ≠ 0.
   - `shell` → `eval '<content>'` (content embedded via Go `%q` so it is bash-safe); failure if exit code ≠ 0.
   - Each command: `echo "Running: <label>"`, then append `[OK]  <label>` or `[FAIL] <label> (exit <n>)` to a results array, bumping a `failures` counter. Continue (run-all).
8. Summary: print each result line, then `N of M commands succeeded.`
9. **macOS dialog (darwin only, emitted at generation time):** build the message from the results array and call `osascript -e 'display dialog "<msg>" with title "<name> <version>" buttons {"OK"} default button 1 with icon note'`. (Results are AppleScript-escaped; newlines join the lines.) Linux builds omit this block entirely.
10. `exit` `1` if `failures > 0`, else `0`.

`ShellRunnerConfig` mirrors `PowerShellRunnerConfig`: `Name, Version, Platform, ExpectedArch, Goos, HasPayload, Commands []CommandSpec` (commands already resolved + `{dst.HOME}`-substituted). `require_admin` is not consumed on Unix (documented).

### Why `set -uo pipefail` without `-e`

Run-all must survive a failing command. `-e` would abort on the first non-zero exit; omitting it lets each command's outcome be captured. `-u`/`pipefail` keep the other safety nets. Each command's exit code is captured explicitly for the `[OK]`/`[FAIL]` verdict.

## `$HOME` Under Root (Install-Mode Caveat)

Install runs as root (`check_root`), so a `path_linux = "$HOME/.config/myapp"` (or `{dst.HOME}/...`) in install-mode would resolve under **root's** home (`/root`), not the end user's. Per-user setup belongs in **run mode**, which runs as the invoking user (so `$HOME` is correct). System paths (`/etc`, `/var/lib`, `/Library/...`) are unaffected. This is documented in CLAUDE.md and the example config; it is not gated (the author wrote `$HOME` deliberately).

## Testing Strategy

TDD throughout. Generated `.sh` text is asserted; additionally the Linux run `.sh` can be **executed** in tests (no GUI), giving stronger coverage than the Windows path which can't run on macOS.

### `config_test.go`
- Override parsing: `exec_windows`, `args_macos`, `shell_linux`, `path_macos`, `system_windows` parse into the struct.
- `CommandConfig.resolveFor`: per-OS override wins when set; falls back to base when unset/empty; slice variants fall back only when the override slice is empty.
- `{dst.HOME}` accepted in path/args/shell fields (including `_<os>` variants); `{dst.HOME}` is not a stray marker anywhere.
- Stray `{dst.*}` rejected in `name`/`items[]`/`services[].exec`/a resolved base path, including via the new `_<os>` field paths.

### `installer/shell_runner_test.go` (new)
- Linux output (`Goos="linux"`): contains `check_arch`, payload extraction iff `HasPayload`, `[OK]`/`[FAIL]` recording, `exit 1` on failure path, and **no** `osascript`.
- macOS output (`Goos="darwin"`): same, plus an `osascript` `display dialog` block with the title and a joined message; absent on linux.
- `exec` command rendered as `"$TMPDIR/<exec>" <args>`; `shell` via `eval '<content>'`; `{dst.HOME}` substituted to `$HOME` in both.
- Run-all: a failing command does not abort the script (the next command's block is still present).

### `main_test.go` / build
- `mode="run"` + `linux-arm64` builds a `.sh` (shebang, embedded run commands, no `osascript`); executing it against a temp payload runs commands and exits `0`/`1`.
- `mode="run"` + `darwin-arm64` builds a `.sh` containing the `osascript` block.
- Per-platform override resolution is applied: a `commands_windows`/`exec_windows` field appears resolved in the Windows build and absent (base used) in the Linux build.
- Build-time guard: `{dst.AppDataFolder}` in the *resolved* linux path fails; `{dst.AppDataFolder}` in `path_windows` does **not** fail a linux build; `{dst.HOME}` in a linux path succeeds.
- Windows run mode (`BuildForWindowsRun`) and install `.sh`/`install.ps1` outputs are byte-identical to today (regression).

### Out of scope (can't fully test on macOS)
The macOS `osascript` dialog actually rendering, and Windows execution. Same gap as existing platform support; the generated text and (for Linux) real execution are the proof.

## Files to Create / Modify

| File | Change |
|------|--------|
| `cmd/onepkg/config.go` | Per-OS override fields on `CommandConfig`/`DirConfig`/`OverlayConfig`; `resolveFor(goos)` resolvers; run-mode validation uses resolved commands; keep structural `Validate` rules. |
| `cmd/onepkg/config_test.go` | Override parse/resolve tests; `{dst.HOME}` acceptance; stray-marker via `_<os>` paths. |
| `cmd/onepkg/markers.go` | Extend `dstSupportedFieldRe` to the `_<os>` field paths; treat `HOME` as a valid cross-platform `{dst.*}` name. |
| `cmd/onepkg/main.go` | Lift the Windows-only run-mode guard; resolve fields per goos before build; route linux/darwin run mode to `BuildForUnixRun`; refine the `{dst.*}` guard to the resolved path and allow `{dst.HOME}`. |
| `cmd/onepkg/main_test.go` | Unix run-mode build tests; per-platform resolution; refined guard; regression. |
| `cmd/onepkg/installer/shell_runner.go` (new) | `GenerateShellRunner`, `ShellRunnerConfig`, `CommandSpec` (reused), dialog emission for darwin. |
| `cmd/onepkg/installer/shell_runner_test.go` (new) | Generated-text tests + linux execution test. |
| `cmd/onepkg/installer/shell.go` | Factor `check_arch` (and any other shared snippet) into a reusable builder used by both install and run `.sh`. |
| `cmd/onepkg/installer/knownfolders.go` | `{dst.HOME}` support: accept in validation/detection, render as `$HOME` (no definition); skip in `EmitKnownFolderDefs`. |
| `cmd/onepkg/installer/knownfolders_test.go` | `{dst.HOME}` tests (all platforms). |
| `docs/config-example.toml` | Run-mode cross-platform example with `_<os>` overrides and `{dst.HOME}`. |
| `CLAUDE.md` | Document per-platform `_<os>` overrides, run mode on linux/macos (linux no dialog, macos osascript), `{dst.HOME}`, the dropped Unix-marker vocabulary, and the `$HOME`-under-root caveat. |
