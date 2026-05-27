# System Overlay Design

## Summary

Allow config to map system paths to bundled items. During installation, if a system path exists on the target machine, the installer copies from the system path into the install directory instead of using the bundled version.

## Config Format

```toml
[[system_overlay]]
system = "/etc/agent/test1.txt"
item = "data/test1.txt"

[[system_overlay]]
system = "/etc/certs"
item = "data/certs"
```

- `system`: absolute path on the target machine. Required.
- `item`: relative path matching an entry in `items`. Required.
- Works for both individual files and directories. Directories are copied recursively.

## Behavior

1. All overlay items are still bundled in the tar payload (always included).
2. After tar extraction, `apply_overlays()` iterates each mapping.
3. If `system` path exists on the target machine → copy it to `$install_dir/$item`, overwriting the extracted version.
4. If `system` path does not exist → keep the bundled version (no warning).
5. If `system` path exists but is unreadable → abort with error.

## Build-Time Changes

### Config struct (`config.go`)

```go
type OverlayConfig struct {
    System string `toml:"system"`
    Item   string `toml:"item"`
}
```

Add `SystemOverlay []OverlayConfig` to `Config`.

### Validation (`config.go`)

- `system` must be an absolute path.
- `item` must match an entry in `items` (exact match or matches a platform-marked item's stripped form).
- No path traversal in `system` (no `..` components).
- `system` must not be a critical system path.

### ShellConfig (`installer/shell.go`)

Add `Overlays []OverlaySpec`:

```go
type OverlaySpec struct {
    System string
    Item   string
}
```

### Main orchestration (`main.go`)

Pass overlay data from parsed config through to `ShellConfig`.

## Install-Time Changes

### Embedded variables

```
OVERLAY_COUNT=3
OVERLAY_0_SYSTEM="/etc/agent/test1.txt"
OVERLAY_0_ITEM="data/test1.txt"
OVERLAY_1_SYSTEM="/etc/agent/test2.txt"
OVERLAY_1_ITEM="data/test2.txt"
OVERLAY_2_SYSTEM="/etc/certs"
OVERLAY_2_ITEM="data/certs"
```

### New bash function: `apply_overlays()`

```bash
apply_overlays() {
    local install_dir="$1"

    if [ "$OVERLAY_COUNT" -eq 0 ]; then
        return
    fi

    for i in $(seq 0 $((OVERLAY_COUNT - 1))); do
        local _var="OVERLAY_${i}_SYSTEM"; local sys_path="${!_var}"
        local _var="OVERLAY_${i}_ITEM"; local item_path="${!_var}"

        if [ -e "$sys_path" ]; then
            echo "Overlay: using system $sys_path → $item_path"
            if [ -d "$sys_path" ]; then
                cp -r "$sys_path/." "$install_dir/$item_path/"
            else
                cp "$sys_path" "$install_dir/$item_path"
            fi
        fi
    done
}
```

Called in `cmd_install` after tar extraction, before VERSION file creation.

## Unchanged Behavior

- `items` list is unchanged — files are always bundled.
- `VERSION` file contains build-time checksums (of the bundled files). After overlay, `verify` may report mismatches for overlay items — this is expected and documents that the file came from the system.
- `uninstall`, `list`, `version` commands are unaffected.

## Open Question for Future Work

A future enhancement could have `apply_overlays()` update the VERSION file's checksums for overlay items after copying from the system path, so `verify` passes cleanly. This is deferred to keep the MVP simple.

## Files Modified

| File | Change |
|------|--------|
| `cmd/onepkg/config.go` | Add `OverlayConfig` struct, `SystemOverlay` field, validation |
| `cmd/onepkg/installer/shell.go` | Add `OverlaySpec`, embed overlay vars, add `apply_overlays()` |
| `cmd/onepkg/main.go` | Wire overlay data from config to shell generator |
| `cmd/onepkg/packager.go` | Pass overlay data through to shell generator (if wiring lives here) |

## Testing

- Unit tests for config parsing and validation of `system_overlay` entries.
- Unit tests for shell generation verifying overlay variables are embedded.
- Integration test: generated script with overlays, verify correct behavior when system path exists/missing.
