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
# Build
cd cmd/onepkg && go build -o onepkg .

# Test all
cd cmd/onepkg && go test ./... -v

# Test specific package
cd cmd/onepkg && go test -run TestParseConfig -v
cd cmd/onepkg && go test ./installer/ -v

# Run
cd cmd/onepkg && go run . -c config.toml
cd cmd/onepkg && go run . -c config.toml --platform linux-arm64 --output ./dist
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

## Rules

- Do not modify `build-package/` — it is a symlink to another project.
- Generated shell scripts use `set -euo pipefail`. Pipe failures need `|| fail=1` suppression.
- Path validation rejects `/..` and leading `..`, but allows `..` within filenames (e.g. `my..backup`).
- MVP scope: Linux/macOS only. Windows `.ps1` and Windows Service are post-MVP.
- Single service only (first `[[services]]` entry). Multiple services is post-MVP.
