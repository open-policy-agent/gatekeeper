package util

import "testing"

func TestMatches(t *testing.T) {
	tcs := []struct {
		name      string
		w         Wildcard
		candidate string
		matches   bool
	}{
		{
			name:      "exact text match",
			w:         Wildcard("kube-system"),
			candidate: "kube-system",
			matches:   true,
		},
		{
			name:      "no glob, wrong text",
			w:         Wildcard("kube-system"),
			candidate: "gatekeeper-system",
			matches:   false,
		},
		{
			name:      "wildcard prefix match",
			w:         Wildcard("kube-*"),
			candidate: "kube-system",
			matches:   true,
		},
		{
			name:      "wildcard prefix doesn't match",
			w:         Wildcard("kube-*"),
			candidate: "gatekeeper-system",
			matches:   false,
		},
		{
			name:      "wildcard suffix match",
			w:         Wildcard("*-system"),
			candidate: "kube-system",
			matches:   true,
		},
		{
			name:      "wildcard suffix doesn't match",
			w:         Wildcard("*-system"),
			candidate: "kube-public",
			matches:   false,
		},
		{
			name:      "missing asterisk yields no wildcard support",
			w:         Wildcard("kube-"),
			candidate: "kube-system",
			matches:   false,
		},
		{
			name:      "wildcard suffix and prefix match",
			w:         Wildcard("*-kube-*"),
			candidate: "test-kube-test",
			matches:   true,
		},
		{
			name:      "no wildcard, only hypens at suffix and prefix",
			w:         Wildcard("-kube-"),
			candidate: "test-kube-test",
			matches:   false,
		},
		{
			name:      "wild card at suffix and prefix, multiple hyphens",
			w:         Wildcard("*-kube-*"),
			candidate: "test-dev-kube-dev-test",
			matches:   true,
		},
		{
			name:      "wild card at suffid and end, multiple hypens, no match",
			w:         Wildcard("*-kube-*"),
			candidate: "my-kub-controller",
			matches:   false,
		},
		{
			name:      "wild card at suffix and prefix, multiple hyphens, no match",
			w:         Wildcard("*-kube-*"),
			candidate: "my-controller-manager",
			matches:   false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			if tc.w.Matches(tc.candidate) != tc.matches {
				if tc.matches {
					t.Errorf("Expected candidate '%v' to match wildcard '%v'", tc.candidate, tc.w)
				} else {
					t.Errorf("Candidate '%v' unexpectedly matched wildcard '%v'", tc.candidate, tc.w)
				}
			}
		})
	}
}
