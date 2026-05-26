package main

import (
	"strings"
	"testing"
)

func TestValidatePlatform(t *testing.T) {
	tests := []struct {
		platform string
		wantErr  string
	}{
		{"linux-arm64", ""},
		{"linux-amd64", ""},
		{"linux-x86_64", "invalid arch"}, // Should use amd64, not x86_64
		{"darwin-arm64", ""},
		{"darwin-amd64", ""},
		{"darwin-x86_64", "invalid arch"},
		{"windows-amd64", "invalid os"}, // MVP only supports linux/darwin
		{"linux", "invalid platform format"},
		{"arm64", "invalid platform format"},
		{"", "invalid platform format"},
		{"linux-arm65", "invalid arch"}, // Typo
		{"linuxo-arm64", "invalid os"},  // Typo
	}

	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			err := ValidatePlatform(tt.platform)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidatePlatform(%q) returned unexpected error: %v", tt.platform, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidatePlatform(%q) returned nil, want error containing %q", tt.platform, tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("ValidatePlatform(%q) error = %q, want error containing %q", tt.platform, err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestParsePlatforms(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
		wantErr  string
	}{
		{"linux-arm64", []string{"linux-arm64"}, ""},
		{"linux-arm64,linux-amd64", []string{"linux-arm64", "linux-amd64"}, ""},
		{"darwin-arm64,darwin-amd64", []string{"darwin-arm64", "darwin-amd64"}, ""},
		{"linux-arm64,invalid-platform", nil, "invalid os"},
		{"", nil, "platform is required"},
		{"linux-arm64,", nil, "invalid platform format"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParsePlatforms(tt.input)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ParsePlatforms(%q) returned unexpected error: %v", tt.input, err)
				}
				if len(result) != len(tt.expected) {
					t.Errorf("ParsePlatforms(%q) returned %d platforms, want %d", tt.input, len(result), len(tt.expected))
				}
				for i, p := range tt.expected {
					if result[i] != p {
						t.Errorf("ParsePlatforms(%q)[%d] = %q, want %q", tt.input, i, result[i], p)
					}
				}
			} else {
				if err == nil {
					t.Errorf("ParsePlatforms(%q) returned nil error, want error containing %q", tt.input, tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("ParsePlatforms(%q) error = %q, want error containing %q", tt.input, err.Error(), tt.wantErr)
				}
			}
		})
	}
}