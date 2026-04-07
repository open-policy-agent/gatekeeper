package oci

import (
	"testing"
)

func TestShouldUsePlainHTTP(t *testing.T) {
	tests := []struct {
		registryHost string
		want         bool
	}{
		{"localhost", true},
		{"localhost:5000", true},
		{"127.0.0.1", true},
		{"127.0.0.1:5000", true},
		{"[::1]", true},
		{"[::1]:5000", true},
		{"ghcr.io", false},
		{"docker.io", false},
		{"myregistry.example.com:443", false},
		{"10.0.0.1:5000", false},
	}

	for _, tt := range tests {
		t.Run(tt.registryHost, func(t *testing.T) {
			got := shouldUsePlainHTTP(tt.registryHost)
			if got != tt.want {
				t.Errorf("shouldUsePlainHTTP(%q) = %v, want %v", tt.registryHost, got, tt.want)
			}
		})
	}
}
