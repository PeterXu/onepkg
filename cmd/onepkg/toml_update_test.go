package main

import (
	"testing"
)

func TestParseValue(t *testing.T) {
	tests := []struct {
		input    string
		expected interface{}
	}{
		{"true", true},
		{"false", false},
		{"42", int64(42)},
		{"0", int64(0)},
		{"-1", int64(-1)},
		{"3.14", float64(3.14)},
		{"0.5", float64(0.5)},
		{"-2.7", float64(-2.7)},
		{"hello", "hello"},
		{"true story", "true story"},
		{"", ""},
		{"123abc", "123abc"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseValue(tt.input)
			if result != tt.expected {
				t.Errorf("ParseValue(%q) = %v (%T), want %v (%T)", tt.input, result, result, tt.expected, tt.expected)
			}
		})
	}
}
