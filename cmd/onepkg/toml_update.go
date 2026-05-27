package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

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

func runTomlUpdate(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: onepkg toml-update [--add] <key> <value> <file>")
	}

	key := args[0]
	value := ParseValue(args[1])
	filePath := args[2]

	// Read and decode the TOML file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", filePath, err)
	}

	var tree map[string]interface{}
	metaData, err := toml.Decode(string(data), &tree)
	if err != nil {
		return fmt.Errorf("decode %s: %w", filePath, err)
	}

	// Check if key exists
	keyParts := strings.Split(key, ".")
	if !keyExists(tree, keyParts) {
		return fmt.Errorf("key %q does not exist (use --add to insert)", key)
	}

	// Set the value
	setKeyValue(tree, keyParts, value)

	// Encode back to TOML
	encoded, err := toml.Marshal(tree)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	// Preserve the original metadata for consistent formatting
	_ = metaData // We'll use this in future for formatting preservation

	if err := os.WriteFile(filePath, encoded, 0644); err != nil {
		return fmt.Errorf("write %s: %w", filePath, err)
	}

	return nil
}

// keyExists checks if a nested key exists in the tree
func keyExists(tree map[string]interface{}, keyParts []string) bool {
	if len(keyParts) == 0 {
		return true
	}

	first := keyParts[0]
	value, exists := tree[first]
	if !exists {
		return false
	}

	if len(keyParts) == 1 {
		return true
	}

	nested, ok := value.(map[string]interface{})
	if !ok {
		return false
	}

	return keyExists(nested, keyParts[1:])
}

// setKeyValue sets a nested key value in the tree
func setKeyValue(tree map[string]interface{}, keyParts []string, value interface{}) {
	if len(keyParts) == 1 {
		tree[keyParts[0]] = value
		return
	}

	first := keyParts[0]
	nested, ok := tree[first].(map[string]interface{})
	if !ok {
		nested = make(map[string]interface{})
		tree[first] = nested
	}

	setKeyValue(nested, keyParts[1:], value)
}
