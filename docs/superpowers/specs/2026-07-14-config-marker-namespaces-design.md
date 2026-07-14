# Config Marker Namespaces — Design Spec

**Date:** 2026-07-14
**Status:** Approved
**Supersedes (refactors):** `2026-07-14-windows-known-folder-variables-design.md` (the known-folder mechanism it introduced stays; only the marker surface changes — see Backward Compatibility).
**Scope:** Give config authors three clearly-namespaced `{...}` markers, each with one unambiguous resolution context:
`{platform}` (legacy, build-time, Go), `{env.NAME}` (host environment, build-time, Go), and `{dst.NAME}` (Windows known folders, resolved on the target at install/run time in PowerShell). This adds the **new** `{env.NAME}` host-variable substitution the user requested, and renames the just-added known-folder markers from `{NAME}` to `{dst.NAME}` so all variable markers share one `{namespace.NAME}` shape.

## Motivation

Two problems, one consistent answer:

1. **Host environment is not reachable from config.** Authors cannot bake a build-time value (a build number, a registry path root, a version tag sourced from CI) into `config.toml` without pre-processing the file. There is no way to say "use `$BUILD_VERSION` for the package version."

2. **The known-folder markers (`{AppDataFolder}`) collide syntactically with potential future markers and are inconsistent with `{platform}` in a way that will only get worse.** Rather than add a second bare `{...}` vocabulary for env vars, unify every variable marker behind a namespace prefix. Known folders are *destination/target* paths, so they move to `{dst.NAME}`; host env becomes `{env.NAME}`; `{platform}` stays bare as the established legacy form.

The build host is macOS/Linux, so **destination** known folders (`{dst.NAME}`) must still resolve on the Windows target at install/run time — that part of the mechanism does not change. Only the marker surface is renamed. **Host env** (`{env.NAME}`) genuinely resolves at build time and produces a literal.

## Requirements

- **`{env.NAME}` host substitution (NEW).** Resolves at build time from the build host's environment, producing a literal value in the parsed config. Applies to *every* string field.
- **Empty/unset `{env.NAME}` fails the build.** No silent empty substitution, no inline defaults — a referenced-but-unset variable is a configuration error that lists every offender.
- **`{dst.NAME}` rename.** The known-folder markers change from `{NAME}` to `{dst.NAME}`, including in run-mode `shell`. One marker syntax (`{dst.NAME}`) everywhere a known folder is used.
- **`{platform}` unchanged.** The existing bare marker keeps its build-time Go behavior exactly as today.
- **Fail fast.** Unknown `{dst.NAME}` names (typos) and `{dst.NAME}` markers used in fields that won't resolve them are config errors.
- **No interaction surprises.** `{env.NAME}` and `{dst.NAME}` can coexist in one field; env resolves first (literal), dst resolves later (PowerShell token).

## Non-Goals

- Inline defaults for `{env.NAME}` (e.g. `{env.NAME:-fallback}`). YAGNI; if a value may be absent, the build should fail and the author should set the env var.
- Expanding `{env.NAME}` into the *target's* environment at install time. `{env.NAME}` is strictly build-host, build-time. Target-side env stays in service `environment` maps.
- New known-folder entries beyond the curated MSI/WiX set.
- A marker for arbitrary shell expansion on Linux/macOS installers. `{dst.NAME}` remains Windows-only (the build guard still rejects marked `dirs`/`overlay` on non-Windows).
- Namespacing `{platform}` into `{platform.X}`. It is a single legacy value; renaming it adds churn for no clarity. It stays `{platform}`.

## The Three Markers

| Marker | Resolves… | Where | How | Applies to |
|---|---|---|---|---|
| `{platform}` | build time | Go (per-platform build) | existing `ResolvePlatformItem` | `items[]` (today); unchanged |
| `{env.NAME}` | build time | Go (`os.Getenv`) | new `(*Config).ExpandEnv` reflection walk | **every** string field |
| `{dst.NAME}` | install/run time | generated PowerShell (`[Environment]::GetFolderPath`) | existing `EmitPathExpr` / new `ReplaceMarkers` | `dirs[].path`, `system_overlay[].system`, run `commands[].args[]`, run `commands[].shell` |

The three regexes are mutually exclusive by prefix, so they never collide:

- `{platform}` — literal substring match (existing `HasPlatformMarker`).
- `{env.NAME}` — `\{env\.([A-Za-z_][A-Za-z0-9_]*)\}`.
- `{dst.NAME}` — `\{dst\.(\w+)\}`.

`NAME` for `env` is an OS environment variable name (permissive: letter/underscore start, then word chars). `NAME` for `dst` is one of the curated `KnownFolders` keys.

## `{env.NAME}` — Host Environment Substitution

### Semantics

Immediately after parsing, a new `(*Config).ExpandEnv() error` walks the entire `Config` value via reflection and, for every settable string scalar, replaces each `{env.NAME}` token with `os.Getenv(NAME)`. The walk covers scalars, `[]string` elements, `map[string]string` values, and nested structs (the full `Config`/`ServiceConfig`/`LinuxService`/`MacOSService`/`WindowsService`/`DirConfig`/`OverlayConfig`/`CommandConfig` tree). Map *keys* and non-string scalars (`bool`, `int`, `*bool`, `*int`) are skipped.

A referenced variable whose value is empty **or** unset is collected. After the walk, if any were collected, `ExpandEnv` returns a single error listing the deduped, sorted offenders:

> `environment variables referenced via {env.NAME} are unset or empty: BUILD_VERSION, REGISTRY_ROOT`

`ExpandEnv` is a one-shot, pre-defaults pass. It mutates the `Config` in place.

### Placement in the build flow

`main.go`:

```
ParseConfig → ExpandEnv  → ApplyDefaults → Validate → (per-platform) BuildForPlatform
```

- **Before `ApplyDefaults`** so defaults apply to resolved values.
- **Before `Validate`** so item-existence and mode checks see resolved values (e.g. `version = "{env.BUILD_VERSION}"`, an item path containing an env root, or `mode = "{env.MODE}"`).
- **Once, not per platform** — host env is constant across the platforms built in one invocation.

### What it does NOT touch

`{platform}` (no `env.` prefix) and `{dst.NAME}` (no `env.` prefix) pass through `ExpandEnv` unchanged. This is what lets a single field contain both: `"{env.REGISTRY_ROOT}\{dst.AppDataFolder}\conf"` → after `ExpandEnv`, `"<root>\{dst.AppDataFolder}\conf"`; later the install/run generators turn the `{dst.AppDataFolder}` token into a PowerShell `$AppDataFolder` reference, with the env-resolved `<root>` as a literal segment.

### Security note

Substituting `{env.NAME}` bakes the build host's value into the packed config (and, if used in a service `environment` map value, into the installed service). That is the feature's purpose (e.g. injecting a build-time token), but authors should treat any sensitive value accordingly — it ships inside the artifact. The mechanism is neutral; the spec documents this rather than gating it.

## `{dst.NAME}` — Known-Folder Rename

### What changes vs. the merged known-folder feature

The resolution mechanism is **unchanged**: markers still become `$<Name> = [Environment]::GetFolderPath([Environment+SpecialFolder]::<Enum>)` definitions emitted into the generated script, and path-string values still become PowerShell `+` concatenations referencing those `$<Name>` vars. The only change is the **marker surface**:

- Old: `{AppDataFolder}` in path-string fields; `$AppDataFolder` in run-mode `shell`.
- New: `{dst.AppDataFolder}` everywhere — path-string fields **and** run-mode `shell`.

### `installer/knownfolders.go` changes

- `markerRe`: `\{(\w+)\}` → `\{dst\.(\w+)\}`. The captured group is still the **bare** folder name (`AppDataFolder`), so `KnownFolders` stays keyed by bare names and `EmitKnownFolderDefs` still emits `$AppDataFolder`.
- `dollarRe` / `KnownFolderDollarVars`: **removed.** Run-mode shell no longer authors `$Name`; it authors `{dst.NAME}`.
- `HasKnownFolderMarker`, `ValidateKnownFolderMarkers`, `KnownFolderMarkers`: signatures unchanged; now detect/validate/collect `{dst.NAME}` (the regex change is internal).
- `EmitKnownFolderDefs`, `EmitPathExpr`: signatures unchanged; `EmitPathExpr` now splits on `{dst.NAME}` and emits `$<bare>` segments — same output shape, different input marker.
- **New `ReplaceMarkers(s string) string`:** global replace of each `{dst.NAME}` (known name) with `$<bare>`; unknown `{dst.X}` left in place (validation rejects them earlier). This is the run-shell counterpart of `EmitPathExpr`: `EmitPathExpr` builds a concatenation *expression* for path-string fields; `ReplaceMarkers` does an *inline* substitution for arbitrary PowerShell.

### Run-mode `shell` unification

In `installer/powershell_runner.go`:

- Known-folder name collection now uses `KnownFolderMarkers(cmd.Shell)` (was `KnownFolderDollarVars(cmd.Shell)`).
- The emitted `Write-Host 'Running:' …` and `Invoke-Expression …` lines pass the shell string through `ReplaceMarkers` so `{dst.AppDataFolder}` becomes `$AppDataFolder` in the PowerShell that actually runs. The `$AppDataFolder` definition at the top of `run.ps1` (unchanged) resolves it in scope under `Invoke-Expression`.

Run `args` are unchanged in mechanism (still `KnownFolderMarkers` + `argPS`/`EmitPathExpr`), only the marker form changes from `{Name}` to `{dst.NAME}`.

### Install-mode path fields

`installer/powershell.go` collects referenced names from `dirs[].path` and `system_overlay[].system` via `KnownFolderMarkers` (now `{dst.NAME}`) and renders them via `EmitPathExpr` — mechanism unchanged, marker form changed. `$DIR_<n>_PATH` / `$OVERLAY_<n>_SYSTEM` still receive the concatenation expression; `CreateDirs` / `ApplyOverlays` are unchanged (the resolved `$<Name>` is rooted on the target, so the existing `IsPathRooted` behavior holds).

## Validation Rules

- **`{env.NAME}`:** validated by `ExpandEnv` — missing/empty → build-failing error (above). No per-field restriction; every string field may use it.
- **`{dst.NAME}` unknown name:** `installer.ValidateKnownFolderMarkers` (called on path fields today) rejects `{dst.X}` where `X` is not in `KnownFolders`, with the supported list. (Error wording updated to read `{dst.X}`.)
- **`{dst.NAME}` outside supported fields (NEW fail-fast):** `{dst.NAME}` is only meaningful where the build emits PowerShell path expressions or does inline marker replacement — i.e. `dirs[].path`, `system_overlay[].system`, run `commands[].args[]`, run `commands[].shell`. A `{dst.NAME}` marker found in *any other* string field (e.g. `name`, `version`, `output_dir`, `items[]`, `exclude[]`, `services[].exec`, a service `working_dir`, `commands[].exec`) is a config error:

  > `{dst.AppDataFolder} is only valid in dirs[].path, system_overlay[].system, and run commands[].args/shell`

  Enforced by a reflection walk over all string fields that records the four supported fields as the allow-list; any `{dst.}` token outside them is reported. (Implementation may reuse the `ExpandEnv` walker's structure; the exact mechanism is the plan's to choose, but the rule — "only these four field types" — is normative.)
- **`{dst.NAME}` in path fields:** existing rules carry over unchanged in intent — `..` traversal still rejected; marked `system_overlay[].system` still skips the Go `IsAbs`/critical-path checks (they cannot be evaluated at build time); marked `dirs[].path` is still naturally exempt from the Go-side critical-path check.
- **Build-time guard (Windows-only, unchanged intent):** `BuildForPlatform` still errors when `goos != "windows"` and `dirs[].path` / `system_overlay[].system` contains a known-folder marker (now `{dst.NAME}`). Run mode is already Windows-only.

## Backward Compatibility

This change is **intentionally breaking** but local-only:

- The known-folder feature (`523a301`) is not pushed; its only consumers are this repo's tests and `docs/config-example.toml`. Both are updated to `{dst.NAME}` in the same change, so no external author is affected.
- **Unmarked configs are byte-identical** in output: `{env.NAME}`/`{dst.NAME}` absent → `ExpandEnv` is a no-op and `EmitPathExpr` still single-quotes bare values. Regression tests guard this.
- `{platform}` is untouched; existing platform-marker tests pass unchanged.
- A config that used the **old bare** `{AppDataFolder}` form will, after this change, treat it as a literal (the new regex requires the `dst.` prefix) rather than a known folder. The stray-marker rule above turns the most likely misuse (in a non-path field) into a clear error; a bare `{AppDataFolder}` left in a *path* field would survive as a literal. Because the feature is local-only and tests/example are migrated, this is acceptable and documented rather than detected.

## Testing Strategy

TDD throughout. Generated `.exe`/`.ps1` still cannot run on macOS, so tests verify generated text and Go-side config logic.

### `config_test.go`
- `ExpandEnv`: substitutes a set env var in a top-level scalar (`version`), in `items[]`, in a service `environment` map value, and in `dirs[].path`; leaves `{dst.NAME}` and `{platform}` untouched in the same field.
- `ExpandEnv` error: a referenced-but-unset `{env.NAME}` returns an error naming it; multiple distinct missing vars are listed deduped/sorted.
- Stray `{dst.NAME}`: rejected in `name`/`items[]`/`services[].exec`; accepted in `dirs[].path`, `overlay[].system`, run `args`, run `shell`.
- Renamed markers: `{dst.AppDataFolder}` validates in path fields and run args; unknown `{dst.Bogus}` errors; the old bare `{AppDataFolder}` no longer validates as a known folder in a path field.
- Regression: unmarked config expands/validates exactly as before.

### `installer/knownfolders_test.go` (updated + new)
- `markerRe` now keys on `{dst.NAME}`: `HasKnownFolderMarker` true for `{dst.AppDataFolder}\x`, false for `{AppDataFolder}` (bare) and `{Foo}`.
- `EmitPathExpr`: `{dst.AppDataFolder}\MyApp` → `$AppDataFolder + '\MyApp'`; bare value still single-quoted literal (regression).
- `ReplaceMarkers` (new): `{dst.AppDataFolder}\x` → `$AppDataFolder\x`; multiple markers; unknown `{dst.X}` left intact.
- `EmitKnownFolderDefs`: unchanged output shape (`$<Name> = [Environment]::GetFolderPath([Environment+SpecialFolder]::<Enum>)`).

### `installer/powershell_runner_test.go` (run)
- `run.ps1` with `{dst.AppDataFolder}` in `shell` emits the `$AppDataFolder` definition once and renders the shell with `$AppDataFolder` (not the marker) in both the log line and `Invoke-Expression`.
- `args` with `{dst.AppDataFolder}\cfg` render as a parenthesized concatenation; unmarked args unchanged (regression).
- No-variable commands emit no `$<Name>` definitions (lean).

### `installer/powershell_test.go` (install)
- Marked `dirs[].path` / `overlay.system` with `{dst.NAME}` emit the `$<Name>` definitions (only referenced) and assign `$DIR_<n>_PATH` / `$OVERLAY_<n>_SYSTEM` as concatenations.
- Unmarked config → no definitions, literal assignments (regression).

### `main_test.go`
- `BuildForPlatform` linux/darwin with a `{dst.NAME}` in `dirs[].path` or `overlay.system` → Windows-only error.
- `windows-amd64` with a marked path succeeds; embedded `install.ps1` contains the definition and concatenation.
- Unmarked config builds on all platforms (regression).

## Files to Create / Modify

| File | Change |
|------|--------|
| `cmd/onepkg/config.go` | New `(*Config).ExpandEnv() error` (reflection walk, `{env.NAME}` → literal, missing → error). New stray-`{dst.NAME}` validation (reject outside the four supported fields). Existing `installer.ValidateKnownFolderMarkers` call sites unchanged in code (marker form handled inside `installer`). |
| `cmd/onepkg/config_test.go` | `ExpandEnv` tests; stray-`{dst.NAME}` tests; rename regression. |
| `cmd/onepkg/installer/knownfolders.go` | `markerRe` → `\{dst\.(\w+)\}`; remove `dollarRe`/`KnownFolderDollarVars`; add `ReplaceMarkers`; update `ValidateKnownFolderMarkers` wording. `EmitPathExpr`/`EmitKnownFolderDefs`/`KnownFolderMarkers`/`HasKnownFolderMarker` updated to the new regex (signatures unchanged). |
| `cmd/onepkg/installer/knownfolders_test.go` | Updated marker tests + new `ReplaceMarkers` tests + regression. |
| `cmd/onepkg/installer/powershell.go` | Reference collection uses `KnownFolderMarkers` on `{dst.NAME}` (mechanism unchanged; marker form changed). |
| `cmd/onepkg/installer/powershell_test.go` | Rename regression. |
| `cmd/onepkg/installer/powershell_runner.go` | `shell` collection via `KnownFolderMarkers`; emit shell via `ReplaceMarkers`. |
| `cmd/onepkg/installer/powershell_runner_test.go` | Run-shell unification + regression. |
| `cmd/onepkg/main.go` | Call `cfg.ExpandEnv()` after `ParseConfig`, before `ApplyDefaults`. |
| `cmd/onepkg/main_test.go` | Guard/expand regression (if a unit seam exists; otherwise covered via `BuildForPlatform`). |
| `docs/config-example.toml` | Update all known-folder examples to `{dst.NAME}`; add `{env.NAME}` example. |
| `CLAUDE.md` | Document the three marker namespaces; note `{dst.NAME}` rename and `{env.NAME}` build-time behavior. |
