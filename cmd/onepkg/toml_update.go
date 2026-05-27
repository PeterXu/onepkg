package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml"
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
	value := ParseValue(args[1])
	filePath := args[2]

	tree, err := toml.LoadFile(filePath)
	if err != nil {
		return fmt.Errorf("load %s: %w", filePath, err)
	}

	if err := setKey(tree, key, value, allowAdd); err != nil {
		return err
	}

	var buf strings.Builder
	if _, err := tree.WriteTo(&buf); err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(buf.String()), 0644); err != nil {
		return fmt.Errorf("write %s: %w", filePath, err)
	}

	return nil
}

// setKey sets a value at the given dotted key path, handling array indices.
// Uses an explicit index loop (not for-range) so it can skip array index parts via i++.
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
			// Peek next part — if numeric, it's an array index
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
				i++ // skip the index part
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
