package util

import "testing"

func TestMatches(t *testing.T) {
	tcs := []struct {
		name      string
		pw        PrefixWildcard
		candidate string
		matches   bool
	}{
		{
			name:      "exact text match",
			pw:        PrefixWildcard("kube-system"),
			candidate: "kube-system",
			matches:   true,
		},
		{
			name:      "no glob, wrong text",
			pw:        PrefixWildcard("kube-system"),
			candidate: "gatekeeper-system",
			matches:   false,
		},
		{
			name:      "wildcard prefix match",
			pw:        PrefixWildcard("kube-*"),
			candidate: "kube-system",
			matches:   true,
		},
		{
			name:      "wildcard prefix doesn't match",
			pw:        PrefixWildcard("kube-*"),
			candidate: "gatekeeper-system",
			matches:   false,
		},
		{
			name:      "missing asterisk yields no wildcard support",
			pw:        PrefixWildcard("kube-"),
			candidate: "kube-system",
			matches:   false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			if tc.pw.Matches(tc.candidate) != tc.matches {
				if tc.matches {
					t.Errorf("Expected candidate '%v' to match wildcard '%v'", tc.candidate, tc.pw)
				} else {
					t.Errorf("Candidate '%v' unexpectedly matched wildcard '%v'", tc.candidate, tc.pw)
				}
			}
		})
	}
}
