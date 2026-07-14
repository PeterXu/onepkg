# Windows `.exe` Extension for Bundled Executables — Design Spec

**Date:** 2026-07-14
**Status:** Approved
**Scope:** When building for Windows, onepkg must store executed bundled files (binaries referenced by `commands[].exec` or `services[].exec`) with an `.exe` extension so Windows can launch them, and must point the generated PowerShell at those `.exe` paths. Today the `{platform}` marker is stripped to an extensionless pack path (`bin/onepkg`), which Windows cannot execute, producing a "How do you want to open this file?" prompt at run/install time.

## Motivation

A Windows run-mode package (`mode = "run"`) with:

```toml
items   = ["bin/onepkg.{platform}"]
[[commands]]
exec    = "bin/onepkg"
args    = ["--add", "...", "{dst.AppDataFolder}/RustDesk/config/RustDesk2.toml"]
```

behaves as follows today:

1. `ResolveItems` resolves `bin/onepkg.{platform}` to source `bin/onepkg.windows-amd64` and strips the marker → pack path **`bin/onepkg`** (no `.exe`).
2. Validation passes: `itemMatches` matches `exec = "bin/onepkg"` against the stripped form (`config.go`).
3. At run time, `run.ps1` executes `& (Join-Path $PayloadDir 'bin/onepkg')`. PowerShell's call operator on a file with no executable extension (`.exe`/`.bat`/`.cmd`/`.ps1`) cannot launch it, so Windows raises the **"How do you want to open this file?"** dialog naming `onepkg`.

The same defect affects install mode: a Windows Service whose `services[].exec` points at an extensionless `{platform}` item would be packed extensionless, and `New-Service`/the resolved binPath could not launch it. `system_overlay[].item` references that name the same file would also miss it once the pack path gains `.exe`.

Windows requires executed files to carry `.exe`. The `{platform}` marker exists precisely so one config builds for every platform; forcing authors to hard-code `.exe` (and to maintain a separate Windows config) defeats that abstraction. onepkg should add the extension automatically, on Windows only.

## Requirements

- **Executed items get `.exe` on Windows.** On `windows-*` builds, a bundled item referenced by `commands[].exec` or `services[].exec` is packed with `.exe` appended to its in-package name when (and only when) that name has no file extension.
- **Non-executed items are untouched.** Data, config, and already-extensioned files (`README`, `myapp.conf`, an explicit `myapp.exe`, a `.ps1`) keep their names verbatim. No `data/README.exe`.
- **PowerShell references the real packed path.** `run.ps1` and `install.ps1` must address the `.exe` path for `commands[].exec`, `services[].exec`, and `system_overlay[].item` when the underlying item was extended.
- **Linux/macOS are byte-identical.** The `.exe` step is gated on `goos == "windows"`; non-Windows builds are unaffected (regression-guarded).
- **Cross-platform config unchanged in spirit.** The author writes `bin/onepkg.{platform}` and `exec = "bin/onepkg"` once; onepkg adds `.exe` on Windows and leaves it off elsewhere.
- **Idempotent / explicit-`.exe` safe.** An author who already writes `items = ["bin/myapp.{platform}.exe"]` and `exec = "bin/myapp.exe"` gets identical output (the item already has an extension, so it is not re-extended).

## Non-Goals

- Renaming the author's **source** artifact. Source lookup (`bin/onepkg.windows-amd64`) is unchanged; only the in-package (zip) name gains `.exe`.
- Extending items that are **not executed**. Overlays that reference a non-exec data file, or any bundled asset not named by an exec, keep their names.
- Removing the run-mode **terminal popup**. The stub is a GUI-subsystem exe (`-H windowsgui`) that spawns `powershell.exe` (console subsystem); Windows allocates a console for the child. `run.ps1` requires that console for its `Read-Host 'Press Enter to exit'` prompt. A future `no_pause`/quiet option is a separate feature.
- Changing config **validation**. Validation is platform-independent and runs once, matching `exec` to items by config strings (pre-`.exe`). The `.exe` mapping is a Windows-only build-time concern; no validation rule changes.
- `.exe` for the install **dirs/overlay path** fields (`{dst.NAME}`). Unrelated mechanism.

## Design

### Data flow (build time, per platform)

Today, in `main()`'s per-platform loop:

```
ResolveItems(configDir, cfg.Items, platform)  -> []ResolvedItem
CollectFilesResolved(configDir, resolved, exclude) -> []FileEntry   // RelPath = PackPath
BuildForPlatform(cfg, platform, entries, ...)  // linux/macOS tar; windows exe
```

New step inserted between `ResolveItems` and `CollectFilesResolved`:

```
resolved := ResolveItems(...)
goos := split(platform)
if goos == "windows" {
    execTargets := execTargetPaths(cfg)              // set of exec strings
    resolved = applyExeExtension(resolved, execTargets)
}
entries := CollectFilesResolved(configDir, resolved, exclude)
...
```

### `execTargetPaths(cfg) map[string]bool`

Returns the set of non-empty exec reference strings from the config:

- Every `cfg.Commands[i].Exec` that is non-empty (run mode).
- `cfg.Services[0].Exec` if non-empty (install mode; MVP is single-service).

These are the **config-form** references — e.g. `"bin/onepkg"` — identical to what `itemMatches` matches against items. Lives in `packager.go` (or `config.go`), package `main`.

### `applyExeExtension(resolved []ResolvedItem, execTargets map[string]bool) []ResolvedItem`

For each `ResolvedItem`:

- Compute the **config-form key** of its pack path: this is `ri.PackPath` *before* any `.exe` is added (i.e. the stripped form, e.g. `bin/onepkg`).
- If `execTargets[key]` is true **and** `filepath.Ext(ri.PackPath) == ""` → set `ri.PackPath += ".exe"`.
- Otherwise leave `ri.PackPath` unchanged.

Matching is by exact equality of the pre-`.exe` pack path against the exec string, which is the same relationship `itemMatches` already guarantees (an exec matches the stripped item form). This means an exec `"bin/onepkg"` selects item `bin/onepkg.{platform}` (stripped `bin/onepkg`); a literal exec `"bin/tool"` selects literal item `bin/tool`.

The extension check uses `filepath.Ext`, so:
- `bin/onepkg` → ext `""` → extended to `bin/onepkg.exe`.
- `bin/myapp.exe` → ext `.exe` → untouched (idempotent for explicit `.exe`).
- `etc/myapp.conf` → ext `.conf` → untouched (not executable anyway, and not an exec target).
- `scripts/setup.ps1` → ext `.ps1` → untouched.

`CollectFilesResolved` then walks the (possibly `.exe`-suffixed) pack paths; shaMap, VERSION, the zip payload, and the tar (Windows does not use tar) all derive from `entries[].RelPath`, so they follow automatically.

### `resolveWindowsItemPath(entries []FileEntry, ref string) string`

Maps a config reference (`commands[].exec`, `services[].exec`, `system_overlay[].item`) to its actual packed path for the generated PowerShell:

1. If any `entries[i].RelPath == ref` → return `ref` (exact; covers already-extensioned and non-extended refs).
2. Else if any `entries[i].RelPath == ref + ".exe"` → return `ref + ".exe"` (the extended item).
3. Else return `ref` unchanged (defensive; with valid config this branch is unreachable for exec/overlay refs, since `applyExeExtension` only ever adds `.exe` and validation guarantees the item exists).

Lives in `packager.go`, package `main`.

### Wiring in the Windows build functions

**Run mode** (`BuildForWindowsRun`): build `commands` with the resolved exec:

```
for _, c := range cfg.Commands {
    commands = append(commands, CommandSpec{
        Exec:  resolveWindowsItemPath(entries, c.Exec),
        Args:  c.Args,
        Shell: c.Shell,
    })
}
```

`run.ps1` then emits `& (Join-Path $PayloadDir 'bin/onepkg.exe')` (via `runnerCommandBlock`, unchanged).

**Install mode** (`BuildForWindows`):

- Service exec: `Exec: resolveWindowsItemPath(entries, svc.Exec)` (was `svc.Exec`).
- Overlay items: `Item: resolveWindowsItemPath(entries, o.Item)` (was `o.Item`), so `ApplyOverlays` copies to/from the real `.exe` path.

Dirs `path`/`overlay system` are unaffected (they are target-path fields, not item references).

### Why this placement is clean

- `ResolveItems` keeps its single responsibility (platform-marker resolution + existence check). The `.exe` concern is a separate pass with its own predicate.
- The extension decision is driven entirely by `execTargets`, derived from config — so the rule is "files that get executed," not a heuristic about item names.
- Reference resolution is one helper reused across commands, service exec, and overlay items, guaranteeing the generated PowerShell and the packed names cannot diverge.
- All paths funnel through `entries[].RelPath`, so VERSION/shaMap/zip consistency is automatic.

## Backward Compatibility

- **Linux/macOS builds:** the `.exe` step is skipped entirely (`goos != "windows"`). Output is byte-identical to today. Regression test required.
- **Windows builds that already use `.exe`:** an item `bin/myapp.{platform}.exe` strips to `bin/myapp.exe` (has an extension) → not re-extended; exec `bin/myapp.exe` resolves exactly. Output identical to today. Regression test required.
- **Windows builds with extensionless executed items (the bug case):** behavior *intentionally changes* — the pack path gains `.exe` and PowerShell points at it. This is the fix; prior output was broken (could not launch).
- **Non-executed extensionless items:** names unchanged (no `.exe`), so existing Windows packages that bundle such files as data are unaffected.
- This feature is local-only / not yet released, so the intentional change carries no migration cost.

## Testing Strategy

TDD throughout. Generated `.exe`/`.ps1` cannot run on macOS, so tests verify packed zip contents (via the zip-building path or `entries`) and generated text, plus Go-side helper logic.

### `packager_test.go` (new)
- `execTargetPaths`: collects command execs and the single service exec; ignores empty execs; empty when no commands/services.
- `applyExeExtension`:
  - Windows + extensionless exec-target `{platform}` item → pack path gains `.exe`.
  - Windows + extensionless exec-target **literal** item → gains `.exe`.
  - Windows + extensionless item that is **not** an exec target (e.g. `data/README`) → **unchanged**.
  - Windows + exec-target item already ending in `.exe` → unchanged (idempotent).
  - Windows + exec-target item with another extension (`.ps1`, `.conf`) → unchanged.
  - Non-Windows call path is not exercised here directly (it's gated in `main.go`); covered by the integration regression below. (If `applyExeExtension` is platform-agnostic and the gate is in the caller, its own tests assert only the map-driven behavior.)
- `resolveWindowsItemPath`:
  - exact match returns `ref`.
  - only `ref + ".exe"` present → returns `ref + ".exe"`.
  - neither present → returns `ref` (fallback).

### `main_test.go` (new + regression)
- **Run mode (the bug):** a config with `items = ["bin/onepkg.{platform}"]`, `mode = "run"`, `commands[].exec = "bin/onepkg"`, built `windows-amd64`:
  - the payload zip contains an entry named `bin/onepkg.exe` (not `bin/onepkg`).
  - the embedded `run.ps1` contains `Join-Path $PayloadDir 'bin/onepkg.exe'` and does **not** contain `Join-Path $PayloadDir 'bin/onepkg'` (bare).
- **Install mode:** a config with a service `exec` referencing an extensionless `{platform}` item, built `windows-amd64`:
  - zip contains the `.exe` entry; embedded `install.ps1` service binPath/exec references the `.exe` path.
  - an `[[system_overlay]]` whose `item` names the same item resolves to the `.exe` path in `install.ps1`.
- **Regression — Linux/macOS:** the same extensionless `{platform}` config built `linux-arm64` (install mode) packs `bin/onepkg` (no `.exe`) and the generated `.sh` is unchanged.
- **Regression — explicit `.exe`:** a Windows config whose item is `bin/myapp.{platform}.exe` and exec `bin/myapp.exe` packs `bin/myapp.exe` once and references it once (no double `.exe`).
- **Regression — non-exec extensionless item:** a Windows run config that also bundles `data/README` (no exec references it) packs `data/README` unchanged (no `.exe`).

### Existing tests
- `powershell_runner_test.go` / `powershell_test.go` assertions that reference `bin/cleanup` / `bin/myapp.exe` remain valid: those tests construct `CommandSpec`/`WindowsServiceConfig` directly with explicit exec strings (already resolved), so they are unaffected by the build-time resolution. Confirm none assert a bare extensionless exec-and-item pairing that the new behavior would change at the integration level.

## Files to Create / Modify

| File | Change |
|------|--------|
| `cmd/onepkg/packager.go` | Add `execTargetPaths(cfg *Config) map[string]bool`, `applyExeExtension(resolved []ResolvedItem, execTargets map[string]bool) []ResolvedItem`, and `resolveWindowsItemPath(entries []FileEntry, ref string) string`. No change to `ResolveItems` or `CollectFilesResolved`. |
| `cmd/onepkg/main.go` | In the per-platform loop, gate the new pass on `goos == "windows"` between `ResolveItems` and `CollectFilesResolved`. In `BuildForWindowsRun`, resolve each `commands[].exec` via `resolveWindowsItemPath`. In `BuildForWindows`, resolve `services[].exec` and each `system_overlay[].item` via `resolveWindowsItemPath`. |
| `cmd/onepkg/packager_test.go` | New unit tests for the three helpers. |
| `cmd/onepkg/main_test.go` | Run/install integration tests + Linux/macOS, explicit-`.exe`, and non-exec-item regressions. |
| `docs/config-example.toml` | Note (comment) that Windows builds auto-append `.exe` to executed extensionless items; no required config change. |
| `CLAUDE.md` | Add a convention bullet documenting the Windows auto-`.exe` behavior. |

## Open Questions

None. The exec-target-only predicate, the helper placement in `packager.go`, and the gating in `main.go` are settled by this design.
