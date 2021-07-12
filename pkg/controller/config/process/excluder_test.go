package process

import (
	"testing"

	"github.com/open-policy-agent/gatekeeper/pkg/util"
)

func TestExactOrPrefixMatch(t *testing.T) {
	tcs := []struct {
		name     string
		nsMap    map[util.PrefixWildcard]bool
		ns       string
		excluded bool
	}{
		{
			name: "exact text match",
			nsMap: map[util.PrefixWildcard]bool{
				"kube-system": true,
				"foobar":      true,
			},
			ns:       "kube-system",
			excluded: true,
		},
		{
			name: "wildcard prefix match",
			nsMap: map[util.PrefixWildcard]bool{
				"kube-*": true,
				"foobar": true,
			},
			ns:       "kube-system",
			excluded: true,
		},
		{
			name: "lack of asterisk prevents globbing",
			nsMap: map[util.PrefixWildcard]bool{
				"kube-": true,
			},
			ns:       "kube-system",
			excluded: false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			if exactOrPrefixMatch(tc.nsMap, tc.ns) != tc.excluded {
				if tc.excluded {
					t.Errorf("Expected ns '%v' to match map: %v", tc.ns, tc.nsMap)
				} else {
					t.Errorf("ns '%v' unexpectedly matched map: %v", tc.ns, tc.nsMap)
				}
			}
		})
	}
}
