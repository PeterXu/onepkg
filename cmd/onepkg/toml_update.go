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

	if existed {
		oldVal := getValueFromTree(tree, key)
		if oldVal == nil {
			return fmt.Errorf("key %q exists but value cannot be retrieved", key)
		}
		// If the existing value is a string, treat the new value as a string too
		// to avoid type mismatch (e.g., "true" → bool, "1.0" → float)
		if _, isStr := oldVal.(string); isStr {
			newVal = args[1]
		}
		newStr := formatTOMLValue(newVal)
		oldStr := formatTOMLValue(oldVal)
		if oldStr == newStr {
			return nil // already set
		}
		result := replaceValueInContent(string(raw), key, oldStr, newStr)
		if result == string(raw) {
			return fmt.Errorf("could not update key %q in-place", key)
		}
		return os.WriteFile(filePath, []byte(result), 0644)
	}

	// --add new key: text-based append (no re-serialization)
	newStr := formatTOMLValue(newVal)
	result, err := appendKeyToContent(string(raw), key, newStr)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, []byte(result), 0644)
}

// sectionTarget represents a TOML section header to navigate to.
type sectionTarget struct {
	header string // e.g. "[[dirs]]" or "[services]"
	index  int    // array occurrence index (0-based); 0 for non-array sections
}

// parseSectionTargets builds the navigation targets from dotted key parts.
func parseSectionTargets(parts []string) []sectionTarget {
	var targets []sectionTarget
	for i := 0; i < len(parts); i++ {
		if _, err := strconv.Atoi(parts[i]); err == nil {
			continue
		}
		if i < len(parts)-1 {
			if idx, err := strconv.Atoi(parts[i+1]); err == nil {
				targets = append(targets, sectionTarget{"[[" + parts[i] + "]]", idx})
			} else {
				targets = append(targets, sectionTarget{"[" + parts[i] + "]", 0})
			}
		}
	}
	return targets
}

// findSectionRange finds the content range [start, end) for the section
// identified by the given key parts. For array indices, navigates to the
// Nth occurrence. Returns (-1, -1) if the section is not found.
func findSectionRange(content string, parts []string) (int, int) {
	targets := parseSectionTargets(parts)
	if len(targets) == 0 {
		return 0, len(content)
	}

	pos := 0
	for _, target := range targets {
		for occ := 0; occ <= target.index; occ++ {
			// Check if header is at current position (e.g., start of file)
			if strings.HasPrefix(content[pos:], target.header) {
				pos += len(target.header)
				continue
			}
			idx := strings.Index(content[pos:], "\n"+target.header)
			if idx == -1 {
				return -1, -1
			}
			pos += idx + 1 + len(target.header) // skip \n + header
		}
	}

	// pos is right after the last section header.
	// Find end: next [ or [[ header, or EOF.
	sectionEnd := len(content)
	nextHeader := regexp.MustCompile(`(?m)^\[`).FindStringIndex(content[pos:])
	if nextHeader != nil && nextHeader[0] > 0 {
		sectionEnd = pos + nextHeader[0]
	}

	return pos, sectionEnd
}

// replaceValueInContent replaces the old value with the new value in raw TOML
// content, navigating to the correct section for nested/array keys.
func replaceValueInContent(content string, key string, oldStr string, newStr string) string {
	parts := strings.Split(key, ".")
	leafKey := parts[len(parts)-1]

	scopeStart, scopeEnd := findSectionRange(content, parts)
	if scopeStart == -1 {
		// No section header found — try the full dotted key as a single line
		// (e.g., "log.to = ..." without a [log] section header)
		return replaceLine(content, key, oldStr, newStr)
	}

	before := content[:scopeStart]
	scoped := content[scopeStart:scopeEnd]
	after := content[scopeEnd:]

	replaced := replaceLine(scoped, leafKey, oldStr, newStr)
	return before + replaced + after
}

// appendKeyToContent appends a new key=value line at the correct position
// in the file, without re-serializing the entire TOML tree.
func appendKeyToContent(content string, key string, value string) (string, error) {
	parts := strings.Split(key, ".")
	leafKey := parts[len(parts)-1]
	newLine := leafKey + " = " + value

	// Top-level key: append to end of file
	if len(parts) == 1 {
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		return content + newLine + "\n", nil
	}

	// Navigate to the parent section
	parentParts := parts[:len(parts)-1]
	scopeStart, scopeEnd := findSectionRange(content, parentParts)
	if scopeStart == -1 {
		return "", fmt.Errorf("parent section for key %q not found", key)
	}

	before := content[:scopeEnd]
	after := content[scopeEnd:]

	if !strings.HasSuffix(before, "\n") {
		before += "\n"
	}

	return before + newLine + "\n" + after, nil
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
