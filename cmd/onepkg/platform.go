package main

import (
	"fmt"
	"strings"
)

var validOS = map[string]bool{
	"linux":   true,
	"darwin":  true,
	"windows": true,
}

var validArch = map[string]bool{
	"amd64": true,
	"arm64": true,
	// "x86":    true, // Post-MVP
}

// ValidatePlatform checks that a platform string is valid format {os}-{arch}
func ValidatePlatform(platform string) error {
	if platform == "" {
		return fmt.Errorf("invalid platform format: empty string")
	}

	parts := strings.SplitN(platform, "-", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid platform format %q, expected {os}-{arch}", platform)
	}

	os, arch := parts[0], parts[1]

	if !validOS[os] {
		return fmt.Errorf("invalid os %q in platform %q (supported: linux, darwin, windows)", os, platform)
	}

	if !validArch[arch] {
		return fmt.Errorf("invalid arch %q in platform %q (supported: amd64, arm64)", arch, platform)
	}

	return nil
}

// ParsePlatforms parses a comma-separated platform string into a list
// Validates each platform and returns error if any are invalid
func ParsePlatforms(input string) ([]string, error) {
	if input == "" {
		return nil, fmt.Errorf("platform is required")
	}

	platforms := strings.Split(input, ",")

	// Validate each platform
	for _, p := range platforms {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, fmt.Errorf("invalid platform format: empty platform in list")
		}
		if err := ValidatePlatform(p); err != nil {
			return nil, err
		}
	}

	// Trim whitespace from result
	result := make([]string, len(platforms))
	for i, p := range platforms {
		result[i] = strings.TrimSpace(p)
	}

	return result, nil
}