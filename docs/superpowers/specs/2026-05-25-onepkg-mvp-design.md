# onepkg MVP Design

A general-purpose self-extracting installer generator that reads `config.toml` and produces platform-specific `.sh`/`.ps1` packages. MVP focuses on Linux/macOS with systemd and launchd service support.

## Config Schema (config.toml)

```toml
name = "autotest"
version = "v1.0.0"
platforms = ["linux-arm64", "linux-amd64", "darwin-arm64"]
output_dir = "packages"

items = [
  "bin/test1",
  "bin/test2",
  "etc/gate.conf",
  "data/",
  "res/",
]

[[services]]
name = "main"          # default: package name
exec = "bin/test1"     # default: first bin/* item
restart = "on-failure" # no | on-failure | always
user = "autotest"      # default: root

[services.linux]
type = "simple"
after = ["network.target"]
restart_sec = 5

[services.macos]
run_at_load = true
keep_alive = true
```

### Defaults

| Field | Default |
|-------|---------|
| `platforms` | Current build platform only |
| `output_dir` | `packages` |
| `services.name` | Package `name` |
| `services.exec` | First `bin/*` item |
| `services.restart` | `on-failure` |
| `services.user` | `root` |

### Validation (before packaging)

- All `items` paths exist
- No path traversal (`../`, absolute paths starting with `/`)
- Service `exec` references an existing item
- `name` is non-empty

## File Structure

```
cmd/onepkg/
├── main.go           # CLI entry point
├── config.go         # TOML parsing, defaults, validation
├── packager.go       # Walk items, compute SHA1, embed files
├── version.go        # Generate VERSION file (TOML format)
└── installer/
    ├── shell.go      # .sh installer generator (Linux/macOS)
    ├── systemd.go    # systemd unit file generator
    └── launchd.go    # launchd plist generator
```

Go module: `module onepkg`, `go 1.21`. Dependency: `github.com/BurntSushi/toml`.

## Packaging Flow

1. Parse `config.toml`, apply defaults, validate
2. For each target platform:
   - Walk all `items` (files and directories), collect file paths relative to config directory
   - Compute SHA1 checksum for each file
   - Generate `VERSION` file content (TOML format)
   - Generate systemd unit or launchd plist from service config
   - Build self-extracting shell script with all files embedded as base64, plus VERSION and service config
   - Write to `{output_dir}/{name}_{version}_{os}_{arch}.sh`

## VERSION File (TOML, generated at packaging)

```toml
name = "autotest"
version = "v1.0.0"
platform = "linux-arm64"
created = "2026-05-25T10:30:00Z"
files = 8

[sha]
"bin/test1" = "a1b2c3..."
"bin/test2" = "d4e5f6..."
"etc/gate.conf" = "789abc..."
"res/t1.png" = "def012..."
```

## Installer Commands (generated .sh)

| Command | Description |
|---------|-------------|
| (no args) | Print help |
| `install` | Install to default directory |
| `install --dir /opt` | Custom install directory |
| `uninstall` | Stop service, remove files and service config |
| `version` | Show installed VERSION file |
| `list` | List installed files with checksums |
| `verify` | Verify checksums against VERSION |

## Default Install Paths

| Platform | Default Path |
|----------|-------------|
| Linux | `/usr/local/{name}` |
| macOS | `/opt/{name}` |

## Output Naming

```
{output_dir}/{name}_{version}_{os}_{arch}.sh
```

Examples:
- `packages/autotest_v1.0.0_linux_arm64.sh`
- `packages/autotest_v1.0.0_darwin_arm64.sh`

## Install Layout

```
/usr/local/autotest/
├── bin/
│   ├── test1
│   └── test2
├── etc/
│   └── gate.conf
├── data/
│   └── ...
├── res/
│   ├── t1.png
│   └── t2.png
└── VERSION
```

## Service Mapping

### Linux systemd

| Config Field | systemd Directive |
|-------------|-------------------|
| `name` | Unit name (`{name}.service`) |
| `exec` | `ExecStart=` |
| `restart` | `Restart=` |
| `user` | `User=` |
| `type` | `Type=` |
| `after` | `After=` |
| `restart_sec` | `RestartSec=` |

### macOS launchd

| Config Field | launchd Key |
|-------------|-------------|
| `name` | Label (`com.{name}`) |
| `exec` | `ProgramArguments` |
| `user` | `UserName` |
| `run_at_load` | `RunAtLoad` |
| `keep_alive` | `KeepAlive` |

## Self-Extracting Script Structure

```
#!/bin/bash
# Header: metadata (name, version, platform, expected arch)
# Architecture check (uname -m)
# Root check for install/uninstall
# Case statement for commands (install, uninstall, version, list, verify)
# Embedded payload: each file as a base64-encoded here-document or variable
```

Files are embedded as base64 and decoded on install. File permissions are preserved by recording mode bits in the payload and applying `chmod` during extraction.

## CLI Usage

```bash
# Build for all platforms listed in config
onepkg -c config.toml

# Build for specific platform
onepkg -c config.toml --platform linux-arm64

# Build for multiple platforms
onepkg -c config.toml --platforms linux-arm64,darwin-arm64

# Specify output directory (overrides config)
onepkg -c config.toml --output ./dist
```

## Post-MVP (deferred)

- Windows `.ps1` installer and Windows Service support
- `upgrade` command (stop service, replace files, restart)
- `exclude` patterns in config
- `preserve_permissions` option
- Multiple services (`[[services]]` array beyond first entry)
- Extended systemd options (timeout, standard output/error, capabilities, limit_nofile)
- Extended launchd options (environment, standard out/error paths)
- Extended Windows service options (recovery)
