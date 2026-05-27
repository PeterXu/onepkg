package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// navigateJSON walks a JSON tree following a dotted key path.
// Returns (parent, leafKey, found).
// parent is the map or slice containing the target.
// leafKey is the final key string (for maps) or index string (for slices).
func navigateJSON(data interface{}, keyPath string) (interface{}, string, bool) {
	parts := splitKeyPath(keyPath)
	if len(parts) == 0 {
		return nil, "", false
	}

	current := data
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		switch v := current.(type) {
		case map[string]interface{}:
			next, ok := v[part]
			if !ok {
				return nil, "", false
			}
			current = next
		case []interface{}:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(v) {
				return nil, "", false
			}
			current = v[idx]
		default:
			return nil, "", false
		}
	}

	leaf := parts[len(parts)-1]

	// Return parent and leaf; found depends on whether leaf exists
	switch v := current.(type) {
	case map[string]interface{}:
		_, exists := v[leaf]
		return v, leaf, exists
	case []interface{}:
		idx, err := strconv.Atoi(leaf)
		if err != nil || idx < 0 || idx >= len(v) {
			return nil, "", false
		}
		return v, leaf, true
	default:
		return nil, "", false
	}
}

// splitKeyPath splits a dotted key path into parts.
func splitKeyPath(key string) []string {
	if key == "" {
		return nil
	}
	return strings.Split(key, ".")
}

// detectIndentation examines raw JSON bytes to determine indentation style.
// Returns the indentation string (e.g., "  ", "    ", "\t").
// Defaults to 2-space if no indentation is detected.
func detectIndentation(raw []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}
		if trimmed == "{" || trimmed == "[" || trimmed == "}" || trimmed == "]" {
			continue
		}
		// Found a content line — extract leading whitespace
		indent := line[:len(line)-len(trimmed)]
		if indent != "" {
			return indent
		}
	}
	return "  " // default
}

// formatJSONValue converts a Go value to its JSON literal string representation.
func formatJSONValue(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return "null"
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case string:
		b, _ := json.Marshal(val)
		return string(b)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// runJsonUpdate executes the json-update subcommand (update-only).
func runJsonUpdate(args []string) error {
	return jsonUpdateCore(args, false)
}

// runJsonUpdateAdd executes the json-update subcommand with --add (upsert).
func runJsonUpdateAdd(args []string) error {
	return jsonUpdateCore(args, true)
}

func jsonUpdateCore(args []string, allowAdd bool) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: onepkg json-update [--add] <key> <value> <file>")
	}

	key := args[0]
	filePath := args[2]

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", filePath, err)
	}

	var data interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("parse %s: %w", filePath, err)
	}

	root, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("root is not a JSON object")
	}

	parent, leaf, found := navigateJSON(root, key)

	if !found && !allowAdd {
		return fmt.Errorf("key %q does not exist (use --add to insert)", key)
	}

	newVal := ParseValue(args[1])

	if found {
		// Get old value for type preservation and no-op check
		var oldVal interface{}
		switch v := parent.(type) {
		case map[string]interface{}:
			oldVal = v[leaf]
		case []interface{}:
			idx, _ := strconv.Atoi(leaf)
			oldVal = v[idx]
		}

		// Type preservation: if old value is a string, treat new value as string
		// Exception: explicit null is never coerced to a string
		if newVal != nil {
			if _, isStr := oldVal.(string); isStr {
				newVal = args[1]
			}
		}

		// No-op check
		oldStr := formatJSONValue(oldVal)
		newStr := formatJSONValue(newVal)
		if oldStr == newStr {
			return nil
		}

		// Set new value
		switch v := parent.(type) {
		case map[string]interface{}:
			v[leaf] = newVal
		case []interface{}:
			idx, _ := strconv.Atoi(leaf)
			v[idx] = newVal
		}
	} else {
		// --add mode: key not found, insert new
		// Only support adding to map (not array append)
		m, ok := parent.(map[string]interface{})
		if !ok {
			return fmt.Errorf("cannot add key %q to non-object parent", key)
		}
		m[leaf] = newVal
	}

	// Re-serialize with original indentation
	indent := detectIndentation(raw)
	result, err := json.MarshalIndent(root, "", indent)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	result = append(result, '\n')

	return os.WriteFile(filePath, result, 0644)
}
