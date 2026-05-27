# toml-update Subcommand Design

## Summary

Add a `toml-update` subcommand to onepkg that modifies key/value pairs in TOML files. Format-preserving via BurntSushi's Tree API.

## CLI Interface

```
onepkg toml-update [--add] <key> <value> <file>
```

- `--add` — upsert mode: update if key exists, add if it doesn't
- Default (no flag) — update only: fail if key doesn't exist
- `key` — Dotted TOML path (e.g. `version`, `services.command`, `dirs.0.path`)
- `value` — New value as string. Auto-detected: `"true"`/`"false"` → bool, numeric → int64, otherwise string
- `file` — Path to TOML file (modified in-place)

## Examples

```bash
# Update version (fails if "version" key doesn't exist)
onepkg toml-update version "1.0.1" config.toml

# Update or add version
onepkg toml-update --add version "1.0.1" config.toml

# Set nested key
onepkg toml-update services.command "/usr/bin/myapp" config.toml

# Set array element
onepkg toml-update dirs.0.path "/var/log/myapp" config.toml
```

## Implementation

- New file `cmd/onepkg/toml_update.go` with `runTomlUpdate(args []string) error`
- Uses `toml.LoadFile()` → `Tree`, `tree.Set(key, value)`, write back
- Value auto-detection order: bool → int64 → string (fallback)
- Array index via dotted path: `dirs.0.path` sets `[[dirs]].path` at index 0
- CLI dispatch in `main.go`: detect `toml-update` as first positional arg

## Error Cases

- File doesn't exist → error
- Key doesn't exist and `--add` not specified → error
- Invalid TOML path → error
- Invalid array index → error

## Testing

- Set top-level key
- Set nested key
- Set array element
- Upsert new key with `--add`
- Fail on missing key without `--add`
- Fail on missing file
- Fail on invalid path
- Value auto-detection (bool, int, string)
- Format preservation (comments and ordering unchanged)
