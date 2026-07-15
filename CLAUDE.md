# CLAUDE.md — Project Conventions for onepkg

General-purpose self-extracting installer generator. Reads `config.toml`, collects files, and produces platform-specific `.sh`/`.ps1` self-extracting packages with service management (systemd, launchd).

## Project Status

Implementation phase. Design spec: `docs/superpowers/specs/2026-05-25-onepkg-mvp-design.md`. Implementation plan: `docs/superpowers/plans/2026-05-26-onepkg-mvp.md`.

## Architecture

```
onepkg/
├── cmd/onepkg/              # New general-purpose tool (this project)
│   ├── main.go              # CLI entry, orchestration
│   ├── config.go            # TOML parsing, defaults, validation
│   ├── packager.go          # File collection, SHA1, tar.gz
│   ├── version.go           # VERSION file generation
│   └── installer/           # Platform-specific generators
│       ├── shell.go         # Self-extracting .sh
│       ├── systemd.go       # systemd unit
│       └── launchd.go       # launchd plist
├── build-package/ -> ../task-mesh/...  # OLD tool, do not modify
└── docs/
    ├── design.md            # Full design (pre-MVP spec)
    └── superpowers/         # Specs and plans
```

## Commands

```bash
# Build (re)builds the embedded Windows stubs via the win-wrapper dependency)
make build

# Test all (generates stubs first, then runs the main + stub-module test suites)
make test

# Test a specific package — stubs must already exist, so run `make win-wrapper`
# (or `make build`) once first; the //go:embed directives fail otherwise.
cd cmd/onepkg && go test -run TestParseConfig -v
cd cmd/onepkg && go test ./installer/ -v

# Run (platform required). Same stub requirement: run `make win-wrapper` first.
cd cmd/onepkg && go run . -c config.toml --platform linux-arm64
cd cmd/onepkg && go run . -c config.toml --platform linux-arm64,linux-amd64,darwin-arm64 --output ./dist
```

## Tech Stack

- Go 1.21, module name `onepkg`
- Dependency: `github.com/BurntSushi/toml`
- No other external dependencies

## Key Conventions

- **TDD**: Write failing test first, then implement. Every function gets a test.
- **Package layout**: `cmd/onepkg/*.go` is `package main`; `cmd/onepkg/installer/` is `package installer`.
- **Generated installer flow**: config.toml → collect files + SHA1 → tar.gz → VERSION → service config → self-extracting .sh
- **Platform naming**: `{os}-{arch}` (e.g. `linux-arm64`), filenames use underscores: `{name}_{version}_{os}_{arch}.sh`
- **Default install paths**: Linux `/usr/local/{name}`, macOS `/opt/{name}`
- **Arch mapping**: Linux arm64 → `aarch64` (`uname -m`), macOS arm64 → `arm64`
- **Platform resolution**: Items use `{platform}` marker (e.g., `bin/myapp.{platform}`) which resolves to platform-specific file (e.g., `bin/myapp.linux-arm64`) and packs with stripped path (`bin/myapp`). Missing platform files fail fast—no fallback.
- **Windows executable extension:** On Windows builds, an item referenced by `commands[].exec` or `services[].exec` is packed with `.exe` appended when its pack path has no extension (so `bin/myapp.{platform}` → `bin/myapp.exe` on Windows). The generated PowerShell addresses the `.exe` path. Non-executed items and already-extensioned items are untouched; Linux/macOS builds are unaffected. Implemented in `ResolveItemsForPlatform` / `applyExeExtension` / `resolveWindowsItemPath` (`packager.go`).
- **Required CLI flag**: `--platform` is required. Comma-separated for multi-platform builds (e.g., `--platform linux-arm64,linux-amd64`).
- **System overlay**: `[[system_overlay]]` entries map absolute system paths to bundled items. At install time, if the system path exists on the target, it overwrites the bundled file. If missing, the bundled version is used. Design spec: `docs/superpowers/specs/2026-05-27-system-overlay-design.md`.
- **Per-platform field overrides:** `commands[].exec|args|shell`, `dirs[].path`, and `system_overlay[].system` each accept a base value plus optional `_<os>` variants (`exec_windows`, `path_macos`, `system_linux`, ...). Effective value for a target OS = override if set, else base. Command type is fixed by the base field; overrides refine the value. `_<os>` is keyed by OS family (linux/darwin/windows), never arch. One config builds many platforms.
- **Run mode (`mode = "run"`):** Cross-platform one-shot package. Windows `.exe` (MessageBox), Linux `.sh` (stdout `[OK]`/`[FAIL]`, no dialog), macOS `.sh` (stdout + `osascript` dialog). `[[commands]]` entries (each exactly one of `exec`+`args` for a bundled file, or `shell` for arbitrary PowerShell/bash) run all (continuing past failures), then a results dialog (Windows/macOS) or stdout tally (Linux) summarizes each command's [OK]/[FAIL] result and the package exits 0/1; no install/service/`VERSION`. `require_admin` gates (does not elevate). `items` optional (shell-only packages omit them). Install mode rejects `[[commands]]`; run mode rejects `services`/`dirs`/`system_overlay`. Run packages reuse the **unchanged** exe stub — `run.ps1` declares and ignores the `-InstallDir`/`-Action` the stub forwards.
- **Config marker namespaces:** three `{...}` markers, mutually exclusive by prefix:
  - `{platform}` — legacy bare marker, build-time in Go, for `items[]` (unchanged).
  - `{env.NAME}` — build-time host-environment substitution via `os.Getenv`, applied to every string field after parse; unset/empty → build-failing error listing offenders. Implemented in `cmd/onepkg/markers.go` (`(*Config).ExpandEnv`), wired through `loadConfig`. No defaults.
  - `{dst.NAME}` — Windows known-folder variables, resolved on the TARGET at install/run time in generated PowerShell via `[Environment]::GetFolderPath` (never in Go). Valid only in `dirs[].path`, `system_overlay[].system`, and run-mode `commands[].args` / `commands[].shell`; anywhere else is a validation error. Curated set in `installer.KnownFolders`. Windows-only (marked `dirs`/`overlay` rejected on non-Windows builds). Design spec: `docs/superpowers/specs/2026-07-14-config-marker-namespaces-design.md`. `{dst.HOME}` is the one cross-platform `{dst.*}` marker — resolves to `$HOME` on Linux, macOS, and Windows. All other `{dst.NAME}` remain Windows-only.

## Rules

- Do not modify `build-package/` — it is a symlink to another project.
- Generated shell scripts use `set -euo pipefail`. Pipe failures need `|| fail=1` suppression.
- Path validation rejects `/..` and leading `..`, but allows `..` within filenames (e.g. `my..backup`).
- Supported platforms: Linux (systemd), macOS (launchd), Windows (Windows Service via sc.exe).
- Windows installers are self-extracting `.exe` files using pre-compiled stubs (see `installer/stubs/`).
- To rebuild stubs after changing `installer/stub-src/wrapper.go`:
  `cd cmd/onepkg/installer/stub-src && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-H windowsgui" -o ../stubs/wrapper-stub-amd64.exe .`
  (repeat with GOARCH=arm64 for the ARM64 stub)
- Single service only (first `[[services]]` entry). Multiple services is post-MVP.
