# Windows Known-Folder Variables — Design Spec

**Date:** 2026-07-14
**Status:** Approved
**Scope:** Let config authors reference a curated set of built-in Windows known-folder variables (e.g. `AppDataFolder`) inside path-bearing config fields, resolved on the Windows target at install/run time — usable in both `mode = "install"` and `mode = "run"` packages.

## Motivation

onepkg's Windows install dir is hardcoded to `C:\Program Files\<name>` and its path fields (`dirs[].path`, `system_overlay[].system`) take literal absolute paths. There is no way to express a **per-user** or **well-known** location such as the roaming AppData folder, ProgramData, or the user's Desktop — because those depend on the target user's profile and cannot be known at build time (the build host is macOS/Linux). Authors today must hand-write the target path, which breaks across users.

This spec adds a small, fixed set of built-in Windows known-folder variables that resolve inside the generated PowerShell at install/run time via `[Environment]::GetFolderPath(...)`, so authors can write `{AppDataFolder}\MyApp\data` and have it resolve correctly on any target.

## Requirements

- **Built-in variable set.** A curated, fixed set of named folders (the MSI/WiX folder names), each mapped to a .NET `SpecialFolder` value. No arbitrary expansion.
- **Install-time resolution.** Variables resolve on the Windows target inside the generated PowerShell — never in Go. The build host has no Windows profile.
- **Both modes.** Usable in `mode = "install"` (path fields) and `mode = "run"` (commands).
- **Fail fast.** Unknown variable names (typos) are config errors at validation time.
- **Backward compatible.** Path fields without variables emit byte-identical output to today.

## Non-Goals

- Relocating the payload install directory (no new `install_dir` field).
- Arbitrary `%ENV%` / environment-variable expansion.
- Folders that require P/Invoke (`SHGetKnownFolderPath`), e.g. `UserProfileFolder`, `WindowsVolume`/system drive. (`[Environment]::GetFolderPath` does not expose these; deferred.)
- Cross-platform equivalents on Linux/macOS (e.g. `$XDG_CONFIG_HOME`). Variables are Windows-only.
- Platform-scoped `dirs`/`overlay` (a marked path is simply rejected on non-Windows builds — see Build-time guard).

## Variable Set

Single source of truth: a Go map `name → SpecialFolder enum`, owned by the `installer` package and exported so `config` (validation) and `main` (build-time guard) share it.

| Variable | `SpecialFolder` enum | Resolves to (typical) |
|---|---|---|
| `AppDataFolder` | `ApplicationData` | `C:\Users\<u>\AppData\Roaming` |
| `LocalAppDataFolder` | `LocalApplicationData` | `C:\Users\<u>\AppData\Local` |
| `CommonAppDataFolder` | `CommonApplicationData` | `C:\ProgramData` |
| `ProgramFilesFolder` | `ProgramFilesX86` | `C:\Program Files (x86)` |
| `ProgramFiles64Folder` | `ProgramFiles` | `C:\Program Files` |
| `WindowsFolder` | `Windows` | `C:\Windows` |
| `SystemFolder` | `System` | `C:\Windows\System32` |
| `DesktopFolder` | `DesktopDirectory` | `C:\Users\<u>\Desktop` |
| `ProgramMenuFolder` | `Programs` | Start Menu\Programs |
| `StartupFolder` | `Startup` | Start Menu\Programs\Startup |
| `PersonalFolder` | `MyDocuments` | `C:\Users\<u>\Documents` |
| `CommonDocumentsFolder` | `CommonDocuments` | `C:\Users\Public\Documents` |

`ProgramFilesFolder` maps to the 32-bit directory (`ProgramFilesX86`); on a 32-bit host that enum value may be empty, which is an acceptable, documented edge (use `ProgramFiles64Folder` for the native directory). `[Environment]::GetFolderPath` returns the path without a trailing separator, so authors append their own `\`.

## Config Schema & Usage

Syntax is `{VarName}` in **path-string** fields (consistent with the existing `{platform}` placeholder; the two never share a field, so there is no parser collision), and `$VarName` (ordinary PowerShell) inside **run-mode `shell`**.

### Install mode — path-string fields

`{Var}` substitution applies to `dirs[].path` and `system_overlay[].system`:

```toml
[[dirs]]
path = "{AppDataFolder}\\MyApp\\data"          # created under AppData, not Program Files
mode = "0755"

[[system_overlay]]
system = "{ProgramFiles64Folder}\\OtherApp\\config.json"   # overlay from another installed app
item   = "config/otherapp.json"
```

### Run mode — `shell` and `args`

Run mode is Windows-only by construction, so the variables are available as PowerShell variables for use in `shell`, and `{Var}` is auto-substituted in `args`:

```toml
[[commands]]
exec = "bin/myapp.exe"
args = ["--config", "{AppDataFolder}\\MyApp\\config.json"]   # auto-substituted to the resolved path

[[commands]]
shell = "Copy-Item profile.json \"$AppDataFolder\\MyApp\\\""  # use $Var directly in PowerShell
```

## Resolution & Emission

All resolution happens in the generated PowerShell. Go builds the right PowerShell expression; it never resolves a folder itself.

### Runtime variable definitions

For each variable actually referenced, emit a definition at the top of the generated script:

```powershell
$AppDataFolder = [Environment]::GetFolderPath([Environment+SpecialFolder]::ApplicationData)
```

- **Install mode** (`install.ps1`): emit definitions only for the variables referenced across `dirs[].path` and `system_overlay[].system` (lean).
- **Run mode** (`run.ps1`): emit definitions for variables referenced across `commands[].shell` (as `$Name` tokens) and `commands[].args` (as `{Name}` markers). Detecting `$Name` in `shell` is a regex scan (`\$<KnownName>` intersected with the known set); only matches in the set are emitted.

### Path-string emission (`dirs[].path`, `system_overlay[].system`)

A value with markers is rendered as a PowerShell `+` concatenation referencing the runtime `$<Name>`; a value without markers is rendered exactly as today (single-quoted literal via `psQuote`).

- `{AppDataFolder}\MyApp\data` → `$AppDataFolder + '\MyApp\data'`
- `{ProgramFiles64Folder}\OtherApp\config.json` → `$ProgramFiles64Folder + '\OtherApp\config.json'`
- bare `C:\Program Files\X` → `'C:\Program Files\X'` (unchanged)

These expressions are assigned to the existing variables the script already reads (`$DIR_<n>_PATH`, `$OVERLAY_<n>_SYSTEM`), e.g.:

```powershell
$DIR_0_PATH = $AppDataFolder + '\MyApp\data'
```

`CreateDirs` / `ApplyOverlays` are **unchanged**: at runtime `$DIR_<n>_PATH` holds the fully-resolved absolute string, so the existing `IsPathRooted` / `Test-Path` logic operates on the resolved value (a marked path resolves to a rooted path and is therefore not re-joined to `$InstallDir`, exactly the desired behavior).

### Run-mode emission

- `shell`: **unchanged** mechanically — the shell string still runs via `Invoke-Expression '<shell>'`. Because the variable definitions sit at the top of `run.ps1`, any `$AppDataFolder` written in the shell resolves in scope when `Invoke-Expression` evaluates it. No rewriting of arbitrary PowerShell.
- `args`: each arg is rendered as a `+` concatenation expression when it contains a marker, else a single-quoted literal. The expression form is used in **both** the `Write-Host 'Running:' …` log line and the `& (Join-Path $PayloadDir '<exec>') …` invocation, so the log shows the resolved value. Example arg `{AppDataFolder}\config.json` → `($AppDataFolder + '\config.json')`.

## Validation Rules

Single source of truth for valid names is `installer.KnownFolders`. `config.go` (package `main`) imports it.

- **Marker detection:** `installer.HasKnownFolderMarker(s)` reports whether `s` contains any `{KnownName}` token.
- **Unknown-marker rejection (fail fast):** in `dirs[].path`, `system_overlay[].system`, and run `args`, any `{X}` where `X` is not a known-folder name is a validation error: `unknown Windows known-folder variable "{X}"; supported: <list>`.
- **`dirs[].path`:** if it has a marker, validate markers (above). The existing `..` traversal check still applies. (The Go-side `IsAbs`/critical-path checks already do not fire for non-absolute Go strings, so a marked path is naturally exempt; no special-casing needed beyond marker validation.)
- **`system_overlay[].system`:** currently **requires** a Go-absolute path and rejects non-absolute. Relax: if `system` has a marker, skip the Go `IsAbs` requirement and the critical-path check (they cannot be evaluated at build time), keep the `..` check, and validate markers. Unmarked `system` keeps today's rules unchanged.
- **Run `exec`:** no special rule — an `exec` containing a marker will not match any bundled item, so `itemMatches` already rejects it.
- **Run `shell`:** **not** scanned (arbitrary PowerShell); validation only ensures `exec`/`shell` exclusivity as today.

## Build-Time Guard (Windows-only)

`dirs` and `overlay` are also emitted into Linux/macOS `.sh` installers, where a Windows known-folder marker is meaningless. Add a guard at the top of `BuildForPlatform` (after parsing `goos`/`arch`, alongside the existing `mode = "run"` non-Windows guard): if `goos != "windows"` and any `dirs[].path` or `system_overlay[].system` contains a known-folder marker, return an error:

> `Windows known-folder variable "AppDataFolder" in dirs[].path is not supported on linux; build for windows or remove it`

Run mode is already Windows-only (guarded), so no extra check is needed for `commands`.

## Backward Compatibility

- Unmarked `dirs[].path` / `system_overlay[].system` emit byte-identical output (single-quoted literals; no `$<Name>` definitions emitted).
- Unmarked run `args` emit byte-identical output.
- Validation of unmarked configs is unchanged.
- The only new top-of-script content in `install.ps1`/`run.ps1` is `$<Name>` definitions, and only when variables are actually referenced.

## Testing Strategy

TDD throughout. As with all Windows support, the generated `.exe` cannot be executed on macOS, so tests verify generated artifact structure and config logic.

### `installer/knownfolders_test.go` (new)
- `KnownFolders` map is non-empty and every enum value is a real `SpecialFolder` name (sanity).
- `HasKnownFolderMarker` true for `{AppDataFolder}\x`, false for plain paths and for unknown `{Foo}` (unknown braces are not "known markers" — detection keys on known names only; rejection of unknown names is validation's job).
- `EmitPathExpr`: bare value → single-quoted literal; marked value → correct `+` concatenation; marker at start/middle/end; multiple markers; marker-only value.
- `EmitKnownFolderDefs` / referenced-name collection: only referenced names emitted; correct `$<Name> = [Environment]::GetFolderPath([Environment+SpecialFolder]::<Enum>)` line.

### `config_test.go`
- Parse/validate accepts known markers in `dirs[].path` and `system_overlay[].system`; rejects unknown `{X}`.
- Marked `system_overlay[].system` no longer fails the "must be absolute" rule; unmarked non-absolute still fails.
- `..` traversal still rejected in marked paths.
- Run `args` with known marker validates; unknown marker in args errors.
- Unmarked configs validate exactly as before (regression).

### `installer/powershell_test.go` (install)
- `GeneratePowerShellInstaller` with a marked `dirs[].path` and marked `overlay.system` emits the `$<Name>` definitions (only referenced ones) and assigns `$DIR_<n>_PATH` / `$OVERLAY_<n>_SYSTEM` as `+` concatenations.
- Unmarked config → no `$<Name>` definitions, literal assignments unchanged (regression).

### `installer/powershell_runner_test.go` (run)
- `run.ps1` with `$AppDataFolder` in `shell` and `{AppDataFolder}` in `args` emits the `$AppDataFolder` definition once and renders the marked arg as a concatenation expression in both the log line and the invocation.
- `shell` with no variable references emits no `$<Name>` definitions (lean).
- Unmarked commands → unchanged output (regression).

### `main_test.go` (build-time guard)
- `BuildForPlatform` for `linux-amd64` / `darwin-arm64` with a marked `dirs[].path` or `overlay.system` returns the Windows-only error.
- `BuildForPlatform` for `windows-amd64` with a marked path succeeds and the embedded `install.ps1` contains the `$<Name>` definition and the concatenation expression.
- Unmarked config builds on all platforms as before (regression).

### Out of scope (can't test on macOS)
Actual `[Environment]::GetFolderPath` values, runtime creation of dirs under AppData, real `Invoke-Expression` of `$AppDataFolder` in shell. Same gap as existing Windows support — documented, deferred to a Windows CI runner.

## Files to Create / Modify

| File | Change |
|------|--------|
| `cmd/onepkg/installer/knownfolders.go` (new) | `KnownFolders` map (source of truth), `HasKnownFolderMarker`, referenced-name collection, `EmitPathExpr`, `EmitKnownFolderDefs`. |
| `cmd/onepkg/installer/knownfolders_test.go` (new) | Tests for the above. |
| `cmd/onepkg/config.go` | Import `installer`; add marker validation to `validateInstall` (dirs/overlay) and `validateRun` (args); relax overlay `IsAbs`/critical-path for marked `system`. |
| `cmd/onepkg/config_test.go` | Validation tests (accept/reject markers, regression). |
| `cmd/onepkg/installer/powershell.go` | Emit `$<Name>` defs for referenced names; render marked `$DIR_<n>_PATH` / `$OVERLAY_<n>_SYSTEM` via `EmitPathExpr`. |
| `cmd/onepkg/installer/powershell_test.go` | Install emission tests + regression. |
| `cmd/onepkg/installer/powershell_runner.go` | Emit `$<Name>` defs for names referenced in `shell`/`args`; render marked args via `EmitPathExpr` in log line + invocation. |
| `cmd/onepkg/installer/powershell_runner_test.go` | Run emission tests + regression. |
| `cmd/onepkg/main.go` | Build-time Windows-only guard for marked `dirs`/`overlay` paths in `BuildForPlatform`. |
| `cmd/onepkg/main_test.go` | Guard test (non-Windows error; Windows success) + regression. |
| `docs/config-example.toml` | Install + run examples using `{Var}` / `$Var`. |
| `CLAUDE.md` | Document the known-folder variable feature and its Windows-only nature. |
