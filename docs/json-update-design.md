# json-update Subcommand — Design Document

## Overview

Add a `json-update` subcommand to onepkg, mirroring the existing `toml-update` subcommand. Provides CLI-based in-place JSON file editing with dotted key navigation, type inference, and upsert support.

## CLI Interface

```
onepkg json-update <key> <value> <file>
onepkg json-update --add <key> <value> <file>
```

- **`<key>`** — Dotted key path: `services.name`, `dirs.0.path`, `servers.0.host`
- **`<value>`** — New value (type-inferred from string, or explicit `null`)
- **`<file>`** — Path to JSON file
- **`--add`** — Upsert: insert if key missing, update if exists. Without `--add`, missing keys error.

### Examples

```bash
# Update existing key
onepkg json-update version "1.0.1" package.json

# Update nested key
onepkg json-update services.web.port 8080 config.json

# Update array element
onepkg json-update dirs.0.path "/var/log/app" config.json

# Set null
onepkg json-update deprecated null package.json

# Add new key
onepkg json-update --add newField "value" package.json
```

## Key Navigation Syntax

| Pattern | Meaning | Example |
|---------|---------|---------|
| `name` | Top-level key | `name` → `{"name": ...}` |
| `a.b.c` | Nested key | `services.web.port` → `{"services":{"web":{"port": ...}}}` |
| `arr.0.field` | Array index + field | `dirs.0.path` → `{"dirs":[{"path": ...}]}` |
| `arr.0` | Array element (primitive) | `tags.0` → `{"tags":["old"]}` |

Same dotted syntax as `toml-update` for consistency. Array indices are bare integers between dots.

## Value Type Inference

`ParseValue()` is reused from `toml_update.go` with one addition:

| Input string | Go type | JSON output |
|--------------|---------|-------------|
| `true` | `bool` | `true` |
| `false` | `bool` | `false` |
| `null` | `nil` | `null` |
| `3.14` | `float64` | `3.14` |
| `42` | `int64` | `42` |
| `"hello"` | `string` | `"hello"` |

Type precedence: `bool` → `null` → `float64` → `int64` → `string`

### Type preservation

Same rule as `toml-update`: if the existing value is a string, the new value is treated as a string even if it looks like another type (e.g., `"true"` stays a string). Explicit `null` is never coerced to a string.

## Architecture

### Approach: Re-serialization (recommended)

Unlike TOML (which has comments and section headers to preserve), JSON has no comments. Re-serialization is acceptable and much simpler than text-replacement.

**Flow:**

```
1. Read file → parse with encoding/json → map[string]interface{}
2. Navigate to key (walk tree with dotted path)
3. Check existence (error if missing and no --add)
4. Check old value == new value → no-op
5. Set new value at target location
6. Re-marshal with original indentation
7. Write back to file
```

### Why not text-replacement?

`toml-update` uses regex text-replacement to preserve comments and formatting. For JSON:
- No comments exist → nothing to lose
- Re-serialization preserves key ordering if we use `json.Decoder` with `UseNumber` and maintain order
- Simpler, less fragile than regex on nested JSON

**Key ordering:** Go's `encoding/json` marshals `map[string]interface{}` in alphabetical order. This is accepted for MVP — keys will be sorted in output.

## File Layout

```
cmd/onepkg/
├── json_update.go          # Core logic
├── json_update_test.go     # Tests
├── toml_update.go          # Existing (ParseValue shared)
└── main.go                 # Add json-update dispatch
```

### Shared code

`ParseValue()` lives in `toml_update.go`. Both subcommands use it. For `null` support, add a `nil` case:

```go
func ParseValue(s string) interface{} {
    if s == "null" {
        return nil
    }
    // ... existing logic
}
```

### New functions in `json_update.go`

| Function | Purpose |
|----------|---------|
| `runJsonUpdate(args)` | Entry point for update-only mode |
| `runJsonUpdateAdd(args)` | Entry point for upsert mode |
| `jsonUpdateCore(args, allowAdd)` | Orchestration: parse, navigate, update, write |
| `navigateJSON(data, keyPath)` | Walk `interface{}` tree, return parent + target |
| `splitKeyPath(key)` | Split dotted key path into parts |
| `formatJSONValue(v)` | Format Go value as JSON literal string |
| `detectIndentation(raw)` | Detect original file indentation (2-space, 4-space, tab) |

## Detailed Function Designs

### jsonUpdateCore

```go
func jsonUpdateCore(args []string, allowAdd bool) error {
    // 1. Validate args length (need key, value, file)
    // 2. Read file
    // 3. Parse JSON into interface{}
    // 4. Navigate to parent of target key
    // 5. Check existence, type preservation
    // 6. Set value
    // 7. Marshal with original indentation
    // 8. Write back
}
```

### navigateJSON

```go
// navigateJSON walks a JSON tree following a dotted key path.
// Returns (parent, leafKey, found).
// parent is the map/slice containing the target.
// leafKey is the final key or index string.
func navigateJSON(data interface{}, keyPath string) (interface{}, string, bool)
```

Navigation rules:
- Split key on `.`
- For each part except the last: descend into map or array
- Numeric parts → array index
- Non-numeric parts → map key
- If any intermediate step fails → return not found

### detectIndentation

```go
// detectIndentation examines raw JSON bytes to determine the
// indentation style (spaces or tabs, and how many).
func detectIndentation(raw []byte) string
```

Looks at the first indented line after `{` or `[` to determine `"\t"`, `"  "`, or `"    "`.

### formatJSONValue

```go
// formatJSONValue converts a Go value to its JSON representation.
func formatJSONValue(v interface{}) string
```

| Go type | Output |
|---------|--------|
| `nil` | `null` |
| `bool` | `true` / `false` |
| `int64` | `42` |
| `float64` | `3.14` |
| `string` | `"escaped string"` |

String escaping uses `json.Marshal`, which handles all JSON escape sequences (`\"`, `\\`, `\n`, `\t`, `\r`, `\f`, `\b`, Unicode control chars).

## main.go Changes

Add dispatch block after the `toml-update` block:

```go
if os.Args[1] == "json-update" {
    var addMode bool
    rest := os.Args[2:]
    if len(rest) > 0 && rest[0] == "--add" {
        addMode = true
        rest = rest[1:]
    }
    var err error
    if addMode {
        err = runJsonUpdateAdd(rest)
    } else {
        err = runJsonUpdate(rest)
    }
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        os.Exit(1)
    }
    return
}
```

Update `printHelp()` to include json-update.

## Test Plan

### Unit tests (all implemented)

| Test | Input | Expected | Status |
|------|-------|----------|--------|
| Top-level string | `name` `"bob"` | `{"name":"bob"}` | Done |
| Nested key | `services.web.port` `8080` | Updates nested value | Done |
| Array element | `dirs.0.path` `"/tmp/log"` | Updates first element | Done |
| Array primitive | `tags.0` `"new"` | Updates array element | Done |
| Bool value | `flag` `true` | `{"flag":true}` | Done |
| Int value | `port` `8080` | `{"port":8080}` | Done |
| Float value | `rate` `3.14` | `{"rate":3.14}` | Done |
| Null value | `old` `null` | `{"old":null}` | Done |
| Type preservation (string) | `name` `true` (old was string) | `{"name":"true"}` | Done |
| Key not found | `missing` `"x"` | Error | Done |
| Key not found with --add | `new` `"x"` | `{"new":"x"}` added | Done |
| Nested key not found with --add | `services.web.host` `"localhost"` | Added to existing parent | Done |
| No-op same value | `name` `"bob"` (already `"bob"`) | File unchanged | Done |
| Array index out of range | `arr.5` `"x"` | Error | Done |
| Invalid JSON file | malformed | Error | Done |
| File not found | bad path | Error | Done |
| Too few args | `"name" "val"` | Error | Done |
| Format preservation (2-space) | 2-space indent | Output uses same indent | Done |
| Format preservation (4-space) | 4-space indent | Output uses same indent | Done |
| Format preservation (tabs) | tab indent | Output uses tabs | Done |
| Empty object with --add | `{}` | Adds key to empty object | Done |
| Deep nesting | `a.b.c.d.e.f` `"updated"` | Updates deeply nested value | Done |
| Value with quotes | `msg` `say "hi"` | Properly escaped | Done |
| Value with backslash | `path` `C:\Users\test` | Properly escaped | Done |
| Root is array | `0` `"x"` on `["a","b","c"]` | Error | Done |

### Edge cases

- Empty JSON file or file with only `{}` — supported via `--add`
- Root is an array (not an object) — returns error ("root is not a JSON object")
- Deeply nested paths: `a.b.c.d.e.f` — supported
- Values containing quotes, backslashes, newlines — handled by `json.Marshal` escaping
- Very large numbers (overflow) — not explicitly handled
- Leading zeros in numbers (`007`) — not explicitly handled
- Unicode values in strings — handled by `json.Marshal`

## Comparison: toml-update vs json-update

| Aspect | toml-update | json-update |
|--------|-------------|-------------|
| External dependency | `go-toml` | None (stdlib `encoding/json`) |
| Edit approach | Text replacement (regex) | Re-serialization |
| Format to preserve | Comments, sections, spacing | Indentation style |
| Sections/headers | Yes (`[section]`) | No |
| Extra value types | — | `null` |
| Array syntax | `dirs.0.path` | `dirs.0.path` (same) |
| Key ordering | Preserved (text-based) | Alphabetical (stdlib default) |
| String escaping | `go-toml` handles | `json.Marshal` handles |

## Status

**Implemented.** All tests passing (33 tests). Implementation commits: `a398c7d..9d1c151`.

## Out of Scope (Post-MVP)

- JSONPath syntax (`$.store.book[0].title`)
- Array append (`arr.-1` to push)
- Object/array value insertion from CLI (e.g., `--type array`, `--type object`)
- Key deletion (`json-update --delete key file`)
- Multiple key updates in one invocation
- Streaming/line-by-line for very large files
- Ordered key preservation in output
