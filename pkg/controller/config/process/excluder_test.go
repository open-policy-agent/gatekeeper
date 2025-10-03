package process

import (
	"sort"
	"testing"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
)

func TestExactOrWildcardMatch(t *testing.T) {
	tcs := []struct {
		name     string
		nsMap    map[wildcard.Wildcard]bool
		ns       string
		excluded bool
	}{
		{
			name: "exact text match",
			nsMap: map[wildcard.Wildcard]bool{
				"kube-system": true,
				"foobar":      true,
			},
			ns:       "kube-system",
			excluded: true,
		},
		{
			name: "wildcard prefix match",
			nsMap: map[wildcard.Wildcard]bool{
				"kube-*": true,
				"foobar": true,
			},
			ns:       "kube-system",
			excluded: true,
		},
		{
			name: "wildcard suffix match",
			nsMap: map[wildcard.Wildcard]bool{
				"*-system": true,
				"foobar":   true,
			},
			ns:       "kube-system",
			excluded: true,
		},
		{
			name: "lack of asterisk prevents globbing",
			nsMap: map[wildcard.Wildcard]bool{
				"kube-": true,
			},
			ns:       "kube-system",
			excluded: false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			if exactOrWildcardMatch(tc.nsMap, tc.ns) != tc.excluded {
				if tc.excluded {
					t.Errorf("Expected ns '%v' to match map: %v", tc.ns, tc.nsMap)
				} else {
					t.Errorf("ns '%v' unexpectedly matched map: %v", tc.ns, tc.nsMap)
				}
			}
		})
	}
}

func TestGetExcludedNamespaces(t *testing.T) {
	tcs := []struct {
		name               string
		matchEntries       []configv1alpha1.MatchEntry
		process            Process
		expectedNamespaces []string
	}{
		{
			name: "single process with multiple namespaces",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-system", "kube-public"},
					Processes:          []string{"audit"},
				},
			},
			process:            Audit,
			expectedNamespaces: []string{"kube-system", "kube-public"},
		},
		{
			name: "wildcard process affects all processes",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-*", "default"},
					Processes:          []string{"*"},
				},
			},
			process:            Webhook,
			expectedNamespaces: []string{"kube-*", "default"},
		},
		{
			name: "multiple match entries for same process",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
					Processes:          []string{"sync"},
				},
				{
					ExcludedNamespaces: []wildcard.Wildcard{"monitoring"},
					Processes:          []string{"sync"},
				},
			},
			process:            Sync,
			expectedNamespaces: []string{"kube-system", "monitoring"},
		},
		{
			name: "empty for non-configured process",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
					Processes:          []string{"audit"},
				},
			},
			process:            Mutation,
			expectedNamespaces: []string{},
		},
		{
			name: "mixed processes with overlapping namespaces",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-system", "app-*"},
					Processes:          []string{"webhook", "mutation-webhook"},
				},
				{
					ExcludedNamespaces: []wildcard.Wildcard{"monitoring"},
					Processes:          []string{"webhook"},
				},
			},
			process:            Webhook,
			expectedNamespaces: []string{"kube-system", "app-*", "monitoring"},
		},
		{
			name:               "empty excluder returns empty list",
			matchEntries:       []configv1alpha1.MatchEntry{},
			process:            Audit,
			expectedNamespaces: []string{},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			excluder := New()
			excluder.Add(tc.matchEntries)

			actualNamespaces := excluder.GetExcludedNamespaces(tc.process)

			// Sort both slices for comparison since map iteration order is not guaranteed
			sort.Strings(actualNamespaces)
			sort.Strings(tc.expectedNamespaces)

			if len(actualNamespaces) != len(tc.expectedNamespaces) {
				t.Errorf("Expected %d namespaces, got %d. Expected: %v, Actual: %v",
					len(tc.expectedNamespaces), len(actualNamespaces), tc.expectedNamespaces, actualNamespaces)
				return
			}

			for i, expected := range tc.expectedNamespaces {
				if actualNamespaces[i] != expected {
					t.Errorf("Mismatch at index %d. Expected: %s, Actual: %s", i, expected, actualNamespaces[i])
				}
			}
		})
	}
}
