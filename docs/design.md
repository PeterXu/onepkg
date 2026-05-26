# General Package Tool Design

A self-extracting installer generator for arbitrary software packages.

## Config.toml Schema

```toml
# Package metadata
name = "autotest"
version = "v1.0.0"

# Build options (platforms via CLI flag, not config)
output_dir = "packages"
preserve_permissions = true
exclude = ["tests/", "*.md", ".git*"]

# Files to package (relative to config.toml location)
# Use {platform} marker for platform-specific binaries:
#   "bin/myapp.{platform}" resolves to "bin/myapp.linux-arm64" for linux-arm64
#   and packs as "bin/myapp" (stripped) in the final package
items = [
  "bin/test1.{platform}",    # platform-specific binary
  "bin/test2.{platform}",    # another platform-specific binary
  "etc/gate.conf",           # same for all platforms
  "data/",                   # directory (recursive)
]

# Services (optional, omit for no service)
[[services]]
name = "main"                    # default: package name
exec = "bin/test1"               # stripped path (no {platform} marker)
args = ["--config", "etc/app.conf"]  # command-line arguments (optional)
restart = "on-failure"           # no | on-failure | always
user = "autotest"                # default: root

[services.linux]                 # systemd (optional)
type = "simple"
after = ["network.target"]
environment = ["ENV=prod"]
restart_sec = 5
limit_nofile = 65535
working_dir = "/var/lib/myapp"   # or relative: "data" -> /usr/local/{name}/data

[services.macos]                 # launchd (optional)
run_at_load = true
keep_alive = true
working_dir = "/opt/myapp/data"  # or relative: "data" -> /opt/{name}/data

[services.windows]               # Windows Service (optional)
recovery_delay_ms = 5000

[[services]]                     # second service
name = "watcher"
exec = "bin/test2"
args = ["--mode", "watch"]       # arguments for second service
restart = "always"
```

## Platform Service Mapping

| Field | Linux (systemd) | macOS (launchd) | Windows |
|-------|-----------------|-----------------|---------|
| `name` | Unit name | Label | Service name |
| `exec` | ExecStart binary | ProgramArguments[0] | binPath |
| `args` | ExecStart arguments | ProgramArguments[1:] | appended to binPath |
| `restart` | Restart= | KeepAlive + RunAtLoad | Service recovery |
| `user` | User= | UserName | LocalSystem (no mapping) |

### Linux systemd Options

| Field | systemd Directive |
|-------|-------------------|
| `type` | Type= (service startup behavior) |
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

**Service Type Values:**

| Type | Behavior | Use Case |
|------|----------|----------|
| `simple` | Service runs in foreground, considered started immediately | Default; most Go servers |
| `forking` | Service forks, parent exits, systemd detects fork complete | Traditional Unix daemons |
| `notify` | Service sends readiness via sd_notify(3) | Services with explicit readiness signaling |
| `oneshot` | Runs once and exits, remains "active" after exit | Setup scripts, one-time tasks |
| `exec` | Like simple, but systemd waits until main binary execs | When pre-exec setup matters |

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

- All `items` paths exist (literal paths checked, `{platform}` items checked at build time)
- No path traversal (`../`, absolute paths starting with `/`)
- Service `exec` references existing item (use stripped path, no `{platform}` marker in exec)
- Platform-specific files exist for each requested platform (fail fast, no fallback)
- `name` doesn't conflict with reserved names

## Default Values

| Field | Default |
|-------|---------|
| `platforms` | Required via CLI (`--platform` flag) |
| `output_dir` | `packages` |
| `preserve_permissions` | `true` |
| `exclude` | `[]` (none) |
| `services.name` | Package `name` |
| `services.exec` | First `bin/*` item (stripped) |
| `services.args` | `[]` (no arguments) |
| `services.restart` | `on-failure` |
| `services.user` | `root` |
| `services.linux.working_dir` | `""` (none - service uses default) |
| `services.macos.working_dir` | `""` (none - service uses default) |

## CLI Usage

```bash
# Build package for specific platform (required)
onepkg -c config.toml --platform linux-arm64

# Build for multiple platforms (comma-separated)
onepkg -c config.toml --platform linux-arm64,linux-amd64,darwin-arm64

# Specify output directory
onepkg -c config.toml --platform linux-arm64 --output ./dist
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