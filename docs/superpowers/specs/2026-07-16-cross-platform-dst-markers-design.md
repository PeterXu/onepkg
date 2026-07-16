# Cross-Platform `{dst.*}` Markers — Design Spec

**Date:** 2026-07-16
**Status:** Approved
**Builds on:** `2026-07-14-config-marker-namespaces-design.md` (`{dst.NAME}`), `2026-07-15-cross-platform-run-mode-and-overrides-design.md` (`{dst.HOME}` as the one cross-platform marker).
**Scope:** Expand the cross-platform `{dst.*}` set from the lone `{dst.HOME}` to four genuinely cross-platform markers — `HOME`, `TMPDIR`, `USER`, `HOSTNAME` — each resolving on Linux, macOS, and Windows at install/run time. The Windows-only known-folder vocabulary is unchanged. `{env.VAR}` is unchanged (build-time, build host, all platforms/modes).

## Motivation

`{dst.HOME}` proved that one destination marker can resolve across all three target OSes. Three more variables are equally universal — every installer/run-package wants the temp directory, the current user, and the host name — and today they can only be reached by writing raw shell (`$TMPDIR`, `$USER`) that is not portable across the bash (Linux/macOS) and PowerShell (Windows) generators. Adding them as `{dst.*}` markers gives one spell that resolves correctly on every target, consistent with the existing `{dst.HOME}` mechanism.

This intentionally does **not** add a curated Unix `{dst.*}` path vocabulary (Config/Data/Cache/Bin, /etc, /opt, …). That was considered and dropped: per-platform `_<os>` overrides plus literal paths cover platform-specific directory layout, and only these four *environment-derived* values are truly cross-platform and worth a name.

## Requirements

- **Four cross-platform `{dst.*}` markers** — `HOME`, `TMPDIR`, `USER`, `HOSTNAME` — resolve on Linux, macOS, and Windows at install/run time (on the target).
- **One spell, per-OS resolution** — the same `{dst.NAME}` resolves to a platform-appropriate token in the generated bash (Linux/macOS) or PowerShell (Windows).
- **`{env.VAR}` untouched** — still resolves once at build time on the build host, every string field, all platforms/modes.
- **Windows-only vocabulary unchanged** — `AppDataFolder`, `ProgramFiles64Folder`, … stay Windows-only (rejected on Linux/macOS by the build guard).
- **Fail fast** — an unknown `{dst.X}` is still a config error; a Windows-only `{dst.*}` on a non-Windows target is still a build error.
- **No regression** — unmarked configs and `{dst.HOME}`-only configs produce byte-identical output to today.

## Non-Goals

- A curated Unix path vocabulary (Config/Data/Cache/Bin, `/etc`, `/opt`, `/var/lib`, …). Per-platform `_<os>` overrides + literal paths cover these; only env-derived values are cross-platform.
- XDG folder semantics beyond what `$HOME`/`$TMPDIR` already carry. (`TMPDIR` honors the target's `$TMPDIR` with a `/tmp` fallback.)
- Target-side `{env.VAR}` resolution. `{env.VAR}` stays build-host/build-time (the prior non-goal stands).
- Splitting the cross-platform class into linux-only vs macos-only. The four markers share identical linux/darwin tokens, so one "unix" resolution covers both.

## The Cross-Platform Set

Four markers, each resolving to an env/automatic variable on the target:

| Marker | Linux / macOS (bash) | Windows (PowerShell) |
|---|---|---|
| `HOME` *(exists)* | `$HOME` | `$HOME` |
| `TMPDIR` | `${TMPDIR:-/tmp}` | `$env:TEMP` |
| `USER` | `${USER:-$(id -un)}` | `$env:USERNAME` |
| `HOSTNAME` | `${HOSTNAME:-$(hostname)}` | `$env:COMPUTERNAME` |

**`set -u` safety:** the install `.sh` (`set -euo pipefail`) and run `.sh` (`set -uo pipefail`) both enable `-u`, which aborts on unset variables. `USER` can be reset by `sudo` (install mode runs as root), and while `HOSTNAME` is a bash builtin it is not guaranteed outside bash, so both use a command-substitution fallback. `$HOME` is always set (root has a home), and `${TMPDIR:-/tmp}` carries its own fallback. The `$(...)` forms expand correctly inside the `%q`-double-quoted assignments and `eval` bodies the generators emit.

**Validity (where usable):** unchanged — the same four `{dst.*}` field types as today: `dirs[].path`, `system_overlay[].system`, run `commands[].args`, run `commands[].shell`, each including its `_<os>` variants. Both install and run modes.

## Resolution Mechanism

The single source of truth is a table in `installer/knownfolders.go`, keyed by name with a unix token and a windows token:

```go
// crossPlatformDst maps a cross-platform {dst.NAME} to its resolution token on
// unix (bash; identical for linux and darwin) and windows (PowerShell). All are
// env/automatic variables on the target, so no runtime definition line is
// emitted for them (unlike the Windows-only KnownFolders, which define
// $<Name> = [Environment]::GetFolderPath(...)).
var crossPlatformDst = map[string]struct{ unix, windows string }{
    "HOME":     {"$HOME", "$HOME"},
    "TMPDIR":   {"${TMPDIR:-/tmp}", "$env:TEMP"},
    "USER":     {"${USER:-$(id -un)}", "$env:USERNAME"},
    "HOSTNAME": {"${HOSTNAME:-$(hostname)}", "$env:COMPUTERNAME"},
}

func crossPlatformDstToken(name, goos string) (string, bool) {
    e, ok := crossPlatformDst[name]
    if !ok {
        return "", false
    }
    if goos == "windows" {
        return e.windows, true
    }
    return e.unix, true // linux, darwin
}
```

This replaces the existing `crossPlatformFolders = map[string]bool{"HOME": true}` (which only recorded membership). Name-predicate helpers adapt:

- `isKnownFolderName(name)` → `name in KnownFolders || name in crossPlatformDst`.
- `isWindowsOnlyFolderName(name)` → `name in KnownFolders` (unchanged semantics; the four cross-platform names are not Windows-only).

### Linux / macOS `.sh` (install and run)

Replace `main.go`'s `substituteHomeUnix` / `substituteHomeUnixArgs` with calls to a new exported `installer.ReplaceCrossPlatformDst(s, goos) string` (owned by `knownfolders.go`, which holds the token table) plus a thin `[]string` slice helper. The table stays in one place; `main.go` just calls it. Applied at the same call sites as today's HOME substitution:

- Install mode (`BuildForPlatform`, non-Windows branch): `cfg.Dirs[i].Path` and `cfg.SystemOverlay[i].System`.
- Run mode (`BuildForUnixRun`): each command's `Exec`, `Args[]`, `Shell` (`Exec` substitution is a defensive no-op — the stray-field check forbids `{dst.*}` in `exec`, so it never contains a marker).

After the build guard runs, no Windows-only `{dst.*}` remains on a unix target, so the substitution only ever encounters the four cross-platform names; any other `{dst.X}` would already have failed validation.

The substituted tokens land inside the generators' `%q`-double-quoted assignments (e.g. `DIR_0_PATH="${TMPDIR:-/tmp}/myapp"`) and `eval %q` bodies, where bash expands them at runtime.

### Windows `.ps1` (install and run)

The four markers are env/automatic PowerShell variables, so they render **inline** with no `[Environment]::GetFolderPath` definition — the same treatment `{dst.HOME}` already gets:

- `EmitPathExpr(s)` (path-string fields → PowerShell concatenation): map `HOME → $HOME`, `TMPDIR → $env:TEMP`, `USER → $env:USERNAME`, `HOSTNAME → $env:COMPUTERNAME` to bare segments; Windows-only names still become `$<Name>` segments. E.g. `{dst.TMPDIR}\myapp` → `$env:TEMP + '\myapp'`.
- `ReplaceMarkers(s)` (run-mode inline `shell`/`args`): same inline mapping for the four names; Windows-only names still become `$<Name>`.
- `EmitKnownFolderDefs(names)`: skip all four cross-platform names (today it skips only `HOME`). Only Windows-only `KnownFolders` entries get a definition line.

## Validation & Build Guard

- **Unknown name:** `ValidateKnownFolderMarkers` still errors on `{dst.X}` where `X` is neither a Windows-only folder nor one of the four cross-platform names. Error wording unchanged.
- **Field allow-list (stray check):** `dstSupportedFieldRe` in `markers.go` is unchanged — no new field types.
- **Build guard (per-OS availability):** unchanged in logic. `HasWindowsOnlyKnownFolderMarker` rejects a `{dst.NAME}` that is a Windows-only folder on a non-Windows target; the four cross-platform names are not Windows-only, so they pass on every target. (No new guard is needed — adding the names to the cross-platform set automatically lets them through.)

## Carried-Over Caveats

- **Install mode runs as root** on Linux/macOS (`check_root`), so `$HOME`/`$USER` resolve under root (e.g. `/root`), not the end user's. `$USER` may also reflect `sudo` reset. Per-user setup belongs in **run mode**, which runs as the invoking user. System paths are unaffected. This is the existing `{dst.HOME}` caveat, now extended to `USER`/`HOSTNAME` where relevant.
- **Security:** these markers resolve to target environment values at install/run time — neutral by design (no host value is baked in). Documented, not gated.

## Testing Strategy

TDD throughout. Generated `.sh` text is asserted; the Linux run `.sh` can additionally be **executed** in tests (no GUI), giving strong coverage of the runtime expansion.

### `installer/knownfolders_test.go`
- `isKnownFolderName`/`isWindowsOnlyFolderName`: `TMPDIR`/`USER`/`HOSTNAME` are known and **not** Windows-only; `HOME` unchanged; `AppDataFolder` is Windows-only.
- `ReplaceCrossPlatformDst` (new): on unix, `{dst.TMPDIR}/x` → `${TMPDIR:-/tmp}/x`, `{dst.USER}` → `${USER:-$(id -un)}`, `{dst.HOSTNAME}` → `${HOSTNAME:-$(hostname)}`; on windows, `{dst.TMPDIR}` → `$env:TEMP`, `{dst.USER}` → `$env:USERNAME`, `{dst.HOSTNAME}` → `$env:COMPUTERNAME`; `{dst.HOME}` unchanged on both; multiple markers in one string; unknown `{dst.X}` left intact.
- `EmitPathExpr`: `{dst.TMPDIR}\x` → `$env:TEMP + '\x'`; `{dst.USER}\cfg` → `$env:USERNAME + '\cfg'`; `{dst.HOME}\x` regression (`$HOME + '\x'`); bare value still single-quoted literal.
- `ReplaceMarkers`: `{dst.HOSTNAME}` → `$env:COMPUTERNAME`; several markers; unknown left intact.
- `EmitKnownFolderDefs`: no definition emitted for any of the four cross-platform names (only Windows-only names define).
- `ValidateKnownFolderMarkers`: `{dst.TMPDIR}` accepted; `{dst.Bogus}` still errors.

### `main_test.go` / build
- **Build guard:** `{dst.TMPDIR}` / `{dst.USER}` in a linux `dirs[].path` **succeeds** (not rejected); `{dst.AppDataFolder}` in a linux path still **fails** (regression); `{dst.HOME}` unchanged.
- **Unix install `.sh`:** `dirs[].path = "{dst.TMPDIR}/myapp"` → embedded `DIR_n_PATH` contains `${TMPDIR:-/tmp}/myapp`; `system_overlay[].system = "{dst.USER}"` analogously.
- **Unix run `.sh` (executable):** a run command with `shell = "printf %s {dst.TMPDIR}"` (or `args` using `{dst.USER}`) builds a `.sh` whose execution expands the token (e.g. prints `/tmp` when `TMPDIR` is unset) and exits 0; macOS `.sh` contains the same token plus its `osascript` block.
- **Windows install/run `.ps1`:** `{dst.TMPDIR}` renders as `$env:TEMP` in the path expression (install) and in `Invoke-Expression`/log (run); `{dst.USER}` as `$env:USERNAME`.
- **Regression:** unmarked config and `{dst.HOME}`-only config produce byte-identical install/run output on all platforms.

## Files to Create / Modify

| File | Change |
|------|--------|
| `cmd/onepkg/installer/knownfolders.go` | Replace `crossPlatformFolders` bool map with `crossPlatformDst` token table + `crossPlatformDstToken(name, goos)`; new `ReplaceCrossPlatformDst(s, goos)`; extend `EmitPathExpr` and `ReplaceMarkers` to render the four markers inline (windows tokens); `EmitKnownFolderDefs` skips all four; update `isKnownFolderName`/`isWindowsOnlyFolderName`. |
| `cmd/onepkg/installer/knownfolders_test.go` | New marker tests above + regression. |
| `cmd/onepkg/main.go` | Replace `substituteHomeUnix`/`substituteHomeUnixArgs` with calls to `installer.ReplaceCrossPlatformDst` (+ a `[]string` slice helper); update install-mode and `BuildForUnixRun` call sites. |
| `cmd/onepkg/main_test.go` | Guard/expand regression; unix install `.sh` token test; unix run `.sh` execution test; windows render test. |
| `docs/config-example.toml` | Add a cross-platform `{dst.TMPDIR}`/`{dst.USER}` example (run or install). |
| `CLAUDE.md` | Document the four cross-platform `{dst.*}` markers and their per-OS tokens; note `{env.VAR}` is build-time only. |

## Backward Compatibility

- **`{dst.HOME}`** behaves exactly as before (same token on all platforms).
- **Unmarked configs** are byte-identical (no markers → no substitution).
- **Windows-only vocabulary** and the build guard are unchanged.
- `{env.VAR}` is unchanged.
- The feature is local-only (not pushed); tests and `docs/config-example.toml` are updated in the same change.
