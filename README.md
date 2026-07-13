# onepkg

A self-extracting installer generator for Go applications and other software packages.

## Overview

onepkg reads a `config.toml` file, collects files, and produces platform-specific self-extracting shell scripts (`.sh`) with service management support (systemd for Linux, launchd for macOS).

## Installation

```bash
cd cmd/onepkg && go build -o onepkg .
```

## Usage

```bash
# Build for a single platform
onepkg -c config.toml --platform linux-arm64

# Build for multiple platforms
onepkg -c config.toml --platform linux-arm64,linux-amd64,darwin-arm64

# Specify output directory
onepkg -c config.toml --platform linux-arm64 --output ./dist
```

**Note:** The `--platform` flag is required. There is no default platform.

## Config.toml

```toml
name = "myapp"
version = "v1.2.0"

# Items to package. Use {platform} marker for platform-specific files.
# The marker resolves to the actual platform (e.g., linux-arm64).
items = [
  "bin/myapp.{platform}",  # resolves to bin/myapp.linux-arm64, packs as bin/myapp
  "etc/config.conf",       # same file for all platforms
  "data/",                 # directory (recursive)
]

# Optional service definition
[[services]]
name = "myapp"
exec = "bin/myapp"         # stripped path (no {platform} marker)
restart = "on-failure"

[services.linux]
type = "simple"
after = ["network.target"]
restart_sec = 5

[services.macos]
run_at_load = true
keep_alive = true
```

See `docs/config-example.toml` for a complete example.

## Platform Marker Resolution

Items with `{platform}` marker:

| Config Item | Platform | Resolved File | Pack Path |
|-------------|----------|---------------|-----------|
| `bin/myapp.{platform}` | `linux-arm64` | `bin/myapp.linux-arm64` | `bin/myapp` |
| `bin/myapp.{platform}` | `darwin-arm64` | `bin/myapp.darwin-arm64` | `bin/myapp` |

The installer looks for the resolved file (e.g., `bin/myapp.linux-arm64`) but places it in the package using the stripped path (`bin/myapp`).

## Output

Generated installers:

```
dist/myapp_v1.2.0_linux_arm64.sh
dist/myapp_v1.2.0_linux_amd64.sh
dist/myapp_v1.2.0_darwin_arm64.sh
```

## Installer Commands

| Command | Description |
|---------|-------------|
| `install` | Install to default directory |
| `install --dir /opt/myapp` | Custom install directory |
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

## Scope

**Platforms:** Linux (systemd), macOS (launchd), and Windows (Windows Service). Windows installers are self-extracting `.exe` files.

## License

MIT