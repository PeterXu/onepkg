package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml"
)

// ParseValue converts a string input to the appropriate Go type:
// bool → float64 → int64 → string (fallback)
func ParseValue(s string) interface{} {
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	if strings.Contains(s, ".") {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	return s
}

// runTomlUpdate executes the toml-update subcommand (update-only).
func runTomlUpdate(args []string) error {
	return tomlUpdateCore(args, false)
}

// runTomlUpdateAdd executes the toml-update subcommand with --add (upsert).
func runTomlUpdateAdd(args []string) error {
	return tomlUpdateCore(args, true)
}

func tomlUpdateCore(args []string, allowAdd bool) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: onepkg toml-update [--add] <key> <value> <file>")
	}

	key := args[0]
	newVal := ParseValue(args[1])
	filePath := args[2]

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", filePath, err)
	}

	tree, err := toml.Load(string(raw))
	if err != nil {
		return fmt.Errorf("parse %s: %w", filePath, err)
	}

	existed := keyExistsInTree(tree, key)

	if !existed && !allowAdd {
		return fmt.Errorf("key %q does not exist (use --add to insert)", key)
	}

	// For existing keys: do targeted text replacement to preserve formatting
	if existed {
		oldVal := getValueFromTree(tree, key)
		if oldVal != nil {
			result := replaceValueInContent(string(raw), key, formatTOMLValue(oldVal), formatTOMLValue(newVal))
			if result != string(raw) {
				return os.WriteFile(filePath, []byte(result), 0644)
			}
		}
	}

	// For new keys or fallback: modify tree and re-serialize
	if err := setKey(tree, key, newVal, allowAdd); err != nil {
		return err
	}

	var buf strings.Builder
	if _, err := tree.WriteTo(&buf); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	return os.WriteFile(filePath, []byte(buf.String()), 0644)
}

// setKey sets a value at the given dotted key path, handling array indices.
func setKey(tree *toml.Tree, key string, value interface{}, allowAdd bool) error {
	parts := strings.Split(key, ".")
	current := interface{}(tree)

	for i := 0; i < len(parts); i++ {
		part := parts[i]
		isLast := i == len(parts)-1

		switch v := current.(type) {
		case *toml.Tree:
			if isLast {
				if !v.Has(part) && !allowAdd {
					return fmt.Errorf("key %q does not exist (use --add to insert)", key)
				}
				v.Set(part, value)
				return nil
			}
			nextIdx, nextErr := strconv.Atoi(parts[i+1])
			if nextErr == nil {
				arr := v.Get(part)
				arrTrees, ok := arr.([]*toml.Tree)
				if !ok {
					return fmt.Errorf("key %q is not an array of tables", strings.Join(parts[:i+1], "."))
				}
				if nextIdx < 0 || nextIdx >= len(arrTrees) {
					return fmt.Errorf("index %d out of range for %q (length %d)", nextIdx, strings.Join(parts[:i+1], "."), len(arrTrees))
				}
				current = arrTrees[nextIdx]
				i++
				continue
			}
			next := v.Get(part)
			if next == nil {
				if allowAdd {
					newTree := toml.Tree{}
					v.Set(part, &newTree)
					current = &newTree
				} else {
					return fmt.Errorf("key %q does not exist (use --add to insert)", strings.Join(parts[:i+1], "."))
				}
			} else {
				current = next
			}
		default:
			return fmt.Errorf("key %q is not a table", strings.Join(parts[:i], "."))
		}
	}

	return nil
}

// keyExistsInTree checks if a dotted key path exists in the tree.
func keyExistsInTree(tree *toml.Tree, key string) bool {
	parts := strings.Split(key, ".")
	current := interface{}(tree)
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		switch v := current.(type) {
		case *toml.Tree:
			if i == len(parts)-1 {
				return v.Has(part)
			}
			nextIdx, nextErr := strconv.Atoi(parts[i+1])
			if nextErr == nil {
				arr := v.Get(part)
				arrTrees, ok := arr.([]*toml.Tree)
				if !ok || nextIdx < 0 || nextIdx >= len(arrTrees) {
					return false
				}
				current = arrTrees[nextIdx]
				i++
				continue
			}
			next := v.Get(part)
			if next == nil {
				return false
			}
			current = next
		default:
			return false
		}
	}
	return true
}

// getValueFromTree retrieves the value at a dotted key path from the tree.
func getValueFromTree(tree *toml.Tree, key string) interface{} {
	parts := strings.Split(key, ".")
	current := interface{}(tree)
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		switch v := current.(type) {
		case *toml.Tree:
			if i == len(parts)-1 {
				return v.Get(part)
			}
			nextIdx, nextErr := strconv.Atoi(parts[i+1])
			if nextErr == nil {
				arr := v.Get(part)
				arrTrees, ok := arr.([]*toml.Tree)
				if !ok || nextIdx < 0 || nextIdx >= len(arrTrees) {
					return nil
				}
				current = arrTrees[nextIdx]
				i++
				continue
			}
			next := v.Get(part)
			if next == nil {
				return nil
			}
			current = next
		default:
			return nil
		}
	}
	return nil
}

// replaceValueInContent replaces the old value with the new value in raw TOML content.
// It finds the line containing oldStr as the value and replaces it with newStr,
// preserving comments, whitespace, and all other content.
func replaceValueInContent(content string, key string, oldStr string, newStr string) string {
	// Find the key's leaf name (last non-numeric part)
	parts := strings.Split(key, ".")
	leafKey := parts[len(parts)-1]
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			leafKey = p
		}
	}

	// For top-level keys or when there's only one occurrence, do a simple line match
	// Match: <whitespace><key><whitespace>=<whitespace><value><rest of line>
	pattern := `(?m)^(\s*` + regexp.QuoteMeta(leafKey) + `\s*=\s*)` +
		regexp.QuoteMeta(oldStr) + `(.*)$`

	re := regexp.MustCompile(pattern)
	return re.ReplaceAllStringFunc(content, func(match string) string {
		subs := re.FindStringSubmatch(match)
		if subs == nil {
			return match
		}
		return subs[1] + newStr + subs[2]
	})
}

// formatTOMLValue formats a Go value as a TOML value string.
func formatTOMLValue(v interface{}) string {
	switch val := v.(type) {
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
		return `"` + val + `"`
	default:
		return fmt.Sprintf("%v", val)
	}
}
