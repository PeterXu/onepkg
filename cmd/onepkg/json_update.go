package main

import (
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
