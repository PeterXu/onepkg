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
// It navigates to the correct section for nested/array keys before replacing.
func replaceValueInContent(content string, key string, oldStr string, newStr string) string {
	parts := strings.Split(key, ".")

	// Build the path of non-index parts to find the right section
	// e.g. "dirs.0.owner" → navigate to second [[dirs]], then match "owner"
	sectionPath := make([]string, 0, len(parts))
	leafKey := parts[len(parts)-1]
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			leafKey = p
		}
	}

	// Collect section headers to navigate to
	for i := 0; i < len(parts); i++ {
		if _, err := strconv.Atoi(parts[i]); err == nil {
			continue // skip numeric indices
		}
		if i < len(parts)-1 {
			// Check if next is numeric (array of tables)
			if idx, err := strconv.Atoi(parts[i+1]); err == nil {
				sectionPath = append(sectionPath, "[["+parts[i]+"]]")
				// Count which occurrence (idx) we need
				_ = idx
			} else {
				sectionPath = append(sectionPath, "["+parts[i]+"]")
			}
		}
	}

	// Find the section in content and replace within it
	if len(sectionPath) == 0 {
		// Top-level key: match anywhere
		return replaceLine(content, leafKey, oldStr, newStr)
	}

	// Navigate to section, then replace within that scope
	scopeStart := 0
	for _, header := range sectionPath {
		idx := strings.Index(content[scopeStart:], "\n"+header)
		if idx == -1 {
			// Try without leading newline (file start)
			if strings.HasPrefix(content[scopeStart:], header) {
				idx = 0
			} else {
				break
			}
		} else {
			idx++ // skip the newline
		}
		scopeStart += idx + len(header)
	}

	if scopeStart >= len(content) {
		scopeStart = 0 // fallback to full content
	}

	// Find the next section boundary (line starting with [) to limit scope
	scopeEnd := len(content)
	nextSection := regexp.MustCompile(`(?m)^\[`).FindStringIndex(content[scopeStart:])
	if nextSection != nil && nextSection[0] > 0 {
		scopeEnd = scopeStart + nextSection[0]
	}

	// Replace within the scoped section
	before := content[:scopeStart]
	scoped := content[scopeStart:scopeEnd]
	after := content[scopeEnd:]

	replaced := replaceLine(scoped, leafKey, oldStr, newStr)
	return before + replaced + after
}

// replaceLine replaces a single key=value line in content.
func replaceLine(content string, key string, oldStr string, newStr string) string {
	pattern := `(?m)^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*)` +
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
		escaped := strings.ReplaceAll(val, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		escaped = strings.ReplaceAll(escaped, "\n", `\n`)
		escaped = strings.ReplaceAll(escaped, "\t", `\t`)
		return `"` + escaped + `"`
	default:
		return fmt.Sprintf("%v", val)
	}
}
