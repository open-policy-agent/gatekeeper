package gator

import "testing"

func TestIsYAMLExtension(t *testing.T) {
	tests := []struct {
		ext      string
		expected bool
	}{
		{ExtYAML, true},
		{ExtYML, true},
		{ExtJSON, false},
		{".txt", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			if got := IsYAMLExtension(tt.ext); got != tt.expected {
				t.Errorf("IsYAMLExtension(%q) = %v, want %v", tt.ext, got, tt.expected)
			}
		})
	}
}

func TestIsSupportedExtension(t *testing.T) {
	tests := []struct {
		ext      string
		expected bool
	}{
		{ExtYAML, true},
		{ExtYML, true},
		{ExtJSON, true},
		{".txt", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			if got := IsSupportedExtension(tt.ext); got != tt.expected {
				t.Errorf("IsSupportedExtension(%q) = %v, want %v", tt.ext, got, tt.expected)
			}
		})
	}
}
