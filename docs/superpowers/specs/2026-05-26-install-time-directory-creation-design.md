# Install-Time Directory Creation Design

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:writing-plans to create implementation plan after reviewing this spec.

**Goal:** Allow packages to create directories at install time, supporting both internal (inside install dir) and external (system paths like /var/log) directories.

**Approach:** New top-level `dirs` config field with path, owner, group, mode attributes. Generated shell installer creates directories during install with proper permissions.

**Tech Stack:** Go 1.21, TOML config, bash installer

---

## Config Schema

Add new top-level `dirs` field to config.toml:

```toml
dirs = [
  # External system directory (absolute path)
  { path = "/var/log/frp", owner = "frpc", mode = "0755" },

  # Relative path -> created inside install dir
  { path = "data", owner = "frpc", mode = "0750" },

  # Minimal - defaults applied
  { path = "/var/lib/myapp" },
]
```

### Field Specification

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `path` | Yes | - | Directory path to create |
| `owner` | No | Service user or `root` | User that owns directory |
| `group` | No | Same as `owner` | Group that owns directory |
| `mode` | No | `0755` | Octal permission mode |

### Path Resolution

- **Absolute path** (`/var/log/frp`): Created exactly as specified
- **Relative path** (`data`, `logs`): Created inside install directory
  - Default install: `/usr/local/{name}/data`
  - Custom --dir: `{base_dir}/{name}/data`

### Default Values Logic

```
owner:
  if specified → use specified
  else if service exists → use service.user
  else → "root"

group:
  if specified → use specified
  else → same as owner

mode:
  if specified → use specified
  else → "0755"
```

---

## Go Config Struct

New types in `cmd/onepkg/config.go`:

```go
type DirConfig struct {
    Path  string `toml:"path"`   // Required
    Owner string `toml:"owner"`  // Optional, default: service user or root
    Group string `toml:"group"`  // Optional, default: same as owner
    Mode  string `toml:"mode"`   // Optional, default: "0755"
}

type Config struct {
    // ... existing fields ...
    Dirs []DirConfig `toml:"dirs"`  // NEW
}
```

---

## Build-Time Validation

In `Config.Validate()`:

### Path Safety

Reject dangerous paths:
- Path traversal: contains `..` component or starts with `..`
- Absolute path traversal: contains `/..`
- System critical paths (blacklist):
  - Specific critical files: `/etc/passwd`, `/etc/shadow`, `/etc/sudoers`, `/etc/sudoers.d/*`
  - Critical directories (and anything under them): `/root`, `/boot`, `/sys`, `/proc`, `/dev`
  - Note: `/etc/myapp/` is ALLOWED (only specific system files blocked)

Allow common data paths:
- `/var/log/*`, `/var/lib/*`, `/var/run/*`, `/var/cache/*`
- `/etc/myapp/*`, `/etc/{name}/*` (package-specific config dirs)
- `/tmp/*`
- Any relative path (resolved inside install dir)

### Field Validation

- `path`: Non-empty, no null bytes, no invalid shell characters
- `mode`: Valid octal string matching regex `^0[0-7]{3,4}$`
- `owner`: Non-empty string if specified
- `group`: Non-empty string if specified

---

## Installer Shell Script

### Generated Script Variables

In `installer/shell.go`, add shell variables for each directory config:

```bash
DIRS_COUNT=2
DIR_0_PATH="/var/log/frp"
DIR_0_OWNER="frpc"
DIR_0_MODE="0755"
...
```

And:
1. New function `create_dirs()` to create directories using these variables
2. Call `create_dirs()` in `cmd_install()` after extracting payload

### DIRS Encoding Format

Instead of TOML (complex to parse in bash), use simple shell variables:

```bash
DIRS_COUNT=2
DIR_0_PATH="/var/log/frp"
DIR_0_OWNER="frpc"
DIR_0_MODE="0755"
DIR_1_PATH="data"
DIR_1_OWNER="frpc"
DIR_1_MODE="0750"
```

This format is directly usable in bash without any parsing logic.

### Shell Function

```bash
create_dirs() {
    local install_dir="$1"

    if [ "$DIRS_COUNT" -eq 0 ] || [ -z "$DIRS_COUNT" ]; then
        return
    fi

    for i in $(seq 0 $((DIRS_COUNT - 1))); do
        eval "dir_path=\"\$DIR_${i}_PATH\""
        eval "dir_owner=\"\$DIR_${i}_OWNER\""
        eval "dir_group=\"\$DIR_${i}_GROUP\""
        eval "dir_mode=\"\$DIR_${i}_MODE\""

        # Resolve relative paths
        if [ "${dir_path:0:1}" != "/" ]; then
            dir_path="$install_dir/$dir_path"
        fi

        mkdir -p "$dir_path"

        if [ -n "$dir_owner" ]; then
            chown "${dir_owner}:${dir_group:-$dir_owner}" "$dir_path"
        fi

        if [ -n "$dir_mode" ]; then
            chmod "$dir_mode" "$dir_path"
        fi
    done
}
```

### Integration Point

In `cmd_install()`:

```bash
mkdir -p "$install_dir"
echo "$PAYLOAD" | base64 -d | tar xzf - -C "$install_dir"
echo "$VERSION_B64" | base64 -d > "$install_dir/VERSION"

# Create directories if configured
if [ "$DIRS_COUNT" -gt 0 ]; then
    create_dirs "$install_dir"
fi

if [ "$SERVICE_TYPE" != "none" ] && [ -n "$SERVICE_B64" ]; then
    echo "Installing service..."
    install_service "$install_dir"
fi
```

---

## Uninstall Behavior

- **External directories**: Left intact (preserve logs, data)
- **Internal directories**: Removed with package (already happens via `rm -rf "$install_dir"`)

No changes needed to `cmd_uninstall()` - existing behavior handles both cases correctly.

---

## Testing

### Unit Tests

1. Parse dirs config with all fields
2. Parse dirs config with minimal fields (defaults applied)
3. Validate rejects dangerous paths
4. Validate accepts safe paths
5. Validate rejects invalid mode strings

### Integration Tests

1. Generate shell script with dirs config
2. Verify DIRS_COUNT and DIR_* variables present
3. Run installer with dirs, verify directories created
4. Verify ownership and permissions applied
5. Verify relative paths resolve correctly
6. Verify external directories preserved on uninstall

---

## Example Usage

```toml
name = "frpc"
version = "v0.61.0"

items = ["bin/frpc", "etc/frpc.toml"]

dirs = [
  { path = "/var/log/frp", owner = "frpc", mode = "0755" },
  { path = "data", owner = "frpc", mode = "0750" },
]

[[services]]
name = "frpc"
exec = "bin/frpc"
args = ["-c", "etc/frpc.toml"]
restart = "on-failure"
user = "frpc"
```

Generated installer creates:
- `/var/log/frp` (owned by frpc:frpc, mode 0755)
- `/usr/local/frpc/data` (owned by frpc:frpc, mode 0750)

---

## Files to Modify

| File | Changes |
|------|---------|
| `cmd/onepkg/config.go` | Add DirConfig struct, add Dirs field to Config |
| `cmd/onepkg/config_test.go` | Add tests for dirs parsing and validation |
| `cmd/onepkg/installer/shell.go` | Add DIR_* variables, create_dirs function, integrate into cmd_install |
| `cmd/onepkg/installer/shell_test.go` | Add tests for dirs in generated script |
| `cmd/onepkg/main.go` | Pass dirs config to shell generator |