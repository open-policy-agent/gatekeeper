package process

import (
	"testing"
)

func TestExactOrPrefixMatch(t *testing.T) {
	tcs := []struct {
		name     string
		nsMap    map[string]bool
		ns       string
		excluded bool
	}{
		{
			name: "exact text match",
			nsMap: map[string]bool{
				"kube-system": true,
				"foobar":      false,
			},
			ns:       "kube-system",
			excluded: true,
		},
		{
			name: "exact text matches false",
			nsMap: map[string]bool{
				"kube-system": true,
				"foobar":      false,
			},
			ns:       "foobar",
			excluded: false,
		},
		{
			name: "wildcard prefix match",
			nsMap: map[string]bool{
				"kube-*": true,
				"foobar": false,
			},
			ns:       "kube-system",
			excluded: true,
		},
		{
			name: "wildcard prefix matches false",
			nsMap: map[string]bool{
				"gatekeeper-*": true,
				"kube-*":       false,
				"foobar":       false,
			},
			ns:       "kube-system",
			excluded: false,
		},
		{
			name: "wildcard prefix mis-matches false",
			nsMap: map[string]bool{
				"gatekeeper-*": true,
				"foobar":       false,
			},
			ns:       "kube-system",
			excluded: false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			match := exactOrPrefixMatch(tc.nsMap, tc.ns)
			if match != tc.excluded {
				t.Errorf("Expected namespace '%v' to match", tc.ns)
			}
		})
	}
}
