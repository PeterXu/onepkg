# General Package Tool Design

A self-extracting installer generator for arbitrary software packages.

## Config.toml Schema

```toml
# Package metadata
name = "autotest"
version = "v1.0.0"

# Build options
platforms = ["linux-arm64", "linux-amd64", "darwin-arm64"]  # or "all"
output_dir = "packages"
preserve_permissions = true
exclude = ["tests/", "*.md", ".git*"]

# Files to package (relative to config.toml location)
items = [
  "bin/test1",
  "bin/test2",
  "etc/gate.conf",
  "data/",
  "res/",
]

# Services (optional, omit for no service)
[[services]]
name = "main"                    # default: package name
exec = "bin/test1"               # default: first bin/* item
restart = "on-failure"           # no | on-failure | always
user = "autotest"                # default: root

[services.linux]                 # systemd (optional)
type = "simple"
after = ["network.target"]
environment = ["ENV=prod"]
restart_sec = 5
limit_nofile = 65535

[services.macos]                 # launchd (optional)
run_at_load = true
keep_alive = true

[services.windows]               # Windows Service (optional)
recovery_delay_ms = 5000

[[services]]                     # second service
name = "watcher"
exec = "bin/test2"
restart = "always"
```

## Platform Service Mapping

| Field | Linux (systemd) | macOS (launchd) | Windows |
|-------|-----------------|-----------------|---------|
| `name` | Unit name | Label | Service name |
| `exec` | ExecStart | ProgramArguments | binPath |
| `restart` | Restart= | KeepAlive + RunAtLoad | Service recovery |
| `user` | User= | UserName | LocalSystem (no mapping) |

### Linux systemd Options

| Field | systemd Directive |
|-------|-------------------|
| `type` | Type= |
| `after` | After= |
| `environment` | Environment= |
| `restart_sec` | RestartSec= |
| `timeout_start` | TimeoutStartSec= |
| `timeout_stop` | TimeoutStopSec= |
| `standard_output` | StandardOutput= |
| `standard_error` | StandardError= |
| `limit_nofile` | LimitNOFILE= |
| `capabilities` | CapabilityBoundingSet= |
| `working_dir` | WorkingDirectory= |

### macOS launchd Options

| Field | launchd Key |
|-------|-------------|
| `run_at_load` | RunAtLoad |
| `keep_alive` | KeepAlive |
| `working_dir` | WorkingDirectory |
| `environment` | EnvironmentVariables |
| `standard_out_path` | StandardOutPath |
| `standard_error_path` | StandardErrorPath |

### Windows Service Options

| Field | Meaning |
|-------|---------|
| `recovery_delay_ms` | Delay before restart (ms) |
| `recovery_reset_period_ms` | Reset failure count after (ms) |
| `recovery_actions` | Actions on failure (restart/reboot/none) |

## VERSION File (generated at packaging)

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

## Installer Commands

| Command | Description |
|---------|-------------|
| (no args) | Print help |
| `install` | Install to default directory |
| `install --dir /opt` | Custom install directory |
| `upgrade` | Stop services, replace files, restart |
| `uninstall` | Remove files and services |
| `version` | Show installed version |
| `list` | List installed files with checksums |
| `verify` | Verify checksums against VERSION |

## Default Install Paths

| Platform | Default Path |
|----------|--------------|
| Linux | `/usr/local/{name}` |
| macOS | `/opt/{name}` |
| Windows | `C:\Program Files\{name}` |

## Output Naming

```
{output_dir}/{name}_{version}_{os}_{arch}.{ext}
```

Examples:
- `packages/autotest_v1.0.0_linux_arm64.sh`
- `packages/autotest_v1.0.0_darwin_arm64.sh`
- `packages/autotest_v1.0.0_windows_amd64.ps1`

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

## Validation (before packaging)

- All `items` paths exist
- No path traversal (`../`, absolute paths starting with `/`)
- Service `exec` references existing item
- Platform binaries exist (if applicable)
- `name` doesn't conflict with reserved names

## Default Values

| Field | Default |
|-------|---------|
| `platforms` | Current build platform only |
| `output_dir` | `packages` |
| `preserve_permissions` | `true` |
| `exclude` | `[]` (none) |
| `services.name` | Package `name` |
| `services.exec` | First `bin/*` item |
| `services.restart` | `on-failure` |
| `services.user` | `root` |

## CLI Usage

```bash
# Build package for all platforms
general-package -c config.toml

# Build for specific platform
general-package -c config.toml --platform linux-arm64

# Build for multiple platforms
general-package -c config.toml --platforms linux-arm64,darwin-arm64

# Specify output directory
general-package -c config.toml --output ./dist
```

## Implementation Location

```
utils/
├── docs/
│   └── design.md          # This document
├── cmd/
│   └── general-package/
│       ├── main.go        # CLI entry point
│       ├── config.go      # TOML parsing
│       ├── packager.go    # Package creation
│       ├── installer.go   # Script generation
│       └── validate.go    # Pre-package validation
└── pkg/
    └── installer/
        ├── shell.go       # .sh generator (Linux/macOS)
        ├── powershell.go  # .ps1 generator (Windows)
        ├── systemd.go     # systemd unit generator
        ├── launchd.go     # launchd plist generator
        └── winservice.go  # Windows service config
```