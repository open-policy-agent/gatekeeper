package process

import (
	"sort"
	"testing"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestWildcardMatch(t *testing.T) {
	tcs := []struct {
		name     string
		patterns []wildcard.Wildcard
		ns       string
		matched  bool
	}{
		{
			name:     "exact text match",
			patterns: []wildcard.Wildcard{"kube-system", "foobar"},
			ns:       "kube-system",
			matched:  true,
		},
		{
			name:     "wildcard prefix match",
			patterns: []wildcard.Wildcard{"kube-*", "foobar"},
			ns:       "kube-system",
			matched:  true,
		},
		{
			name:     "wildcard suffix match",
			patterns: []wildcard.Wildcard{"*-system", "foobar"},
			ns:       "kube-system",
			matched:  true,
		},
		{
			name:     "lack of asterisk prevents globbing",
			patterns: []wildcard.Wildcard{"kube-"},
			ns:       "kube-system",
			matched:  false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			if wildcardMatch(tc.patterns, tc.ns) != tc.matched {
				if tc.matched {
					t.Errorf("Expected ns '%v' to match patterns: %v", tc.ns, tc.patterns)
				} else {
					t.Errorf("ns '%v' unexpectedly matched patterns: %v", tc.ns, tc.patterns)
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
		{
			name: "entries with GVK filters are excluded from GetExcludedNamespaces",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
					Processes:          []string{"audit"},
				},
				{
					ExcludedNamespaces: []wildcard.Wildcard{"monitoring"},
					Kinds:              []string{"ConfigMap"},
					Processes:          []string{"audit"},
				},
			},
			process:            Audit,
			expectedNamespaces: []string{"kube-system"},
		},
		{
			name: "entries with namespace selector are excluded from GetExcludedNamespaces",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"default"},
					Processes:          []string{"webhook"},
				},
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "dev"},
					},
					Processes: []string{"webhook"},
				},
			},
			process:            Webhook,
			expectedNamespaces: []string{"default"},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			excluder := New()
			if err := excluder.Add(tc.matchEntries); err != nil {
				t.Fatalf("unexpected error from Add: %v", err)
			}

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

func makeObj(group, version, kind, name, namespace string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: version, Kind: kind})
	obj.SetName(name)
	obj.SetNamespace(namespace)
	return obj
}

func makeNamespace(name string, labels map[string]string) *unstructured.Unstructured {
	obj := makeObj("", "v1", "Namespace", name, "")
	obj.SetLabels(labels)
	return obj
}

func TestIsNamespaceExcluded(t *testing.T) {
	tcs := []struct {
		name         string
		matchEntries []configv1alpha1.MatchEntry
		process      Process
		obj          *unstructured.Unstructured
		excluded     bool
	}{
		{
			name: "namespace excluded by name pattern",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
					Processes:          []string{"*"},
				},
			},
			process:  Audit,
			obj:      makeNamespace("kube-system", nil),
			excluded: true,
		},
		{
			name: "namespace not excluded",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
					Processes:          []string{"*"},
				},
			},
			process:  Audit,
			obj:      makeNamespace("default", nil),
			excluded: false,
		},
		{
			name: "pod in excluded namespace",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
					Processes:          []string{"*"},
				},
			},
			process:  Webhook,
			obj:      makeObj("", "v1", "Pod", "my-pod", "kube-system"),
			excluded: true,
		},
		{
			name: "pod not in excluded namespace",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
					Processes:          []string{"*"},
				},
			},
			process:  Webhook,
			obj:      makeObj("", "v1", "Pod", "my-pod", "default"),
			excluded: false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			excluder := New()
			if err := excluder.Add(tc.matchEntries); err != nil {
				t.Fatalf("unexpected error from Add: %v", err)
			}
			excluded, err := excluder.IsNamespaceExcluded(tc.process, tc.obj)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if excluded != tc.excluded {
				t.Errorf("expected excluded=%v, got %v", tc.excluded, excluded)
			}
		})
	}
}

func TestIsObjectExcludedWithGVK(t *testing.T) {
	tcs := []struct {
		name         string
		matchEntries []configv1alpha1.MatchEntry
		process      Process
		obj          *unstructured.Unstructured
		nsLabels     map[string]string
		excluded     bool
	}{
		{
			name: "configmap excluded by kind filter",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
					Kinds:              []string{"ConfigMap"},
					Processes:          []string{"*"},
				},
			},
			process:  Webhook,
			obj:      makeObj("", "v1", "ConfigMap", "my-cm", "kube-system"),
			excluded: true,
		},
		{
			name: "pod not excluded by configmap kind filter",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
					Kinds:              []string{"ConfigMap"},
					Processes:          []string{"*"},
				},
			},
			process:  Webhook,
			obj:      makeObj("", "v1", "Pod", "my-pod", "kube-system"),
			excluded: false,
		},
		{
			name: "apiGroup filter matches",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"default"},
					APIGroups:          []string{"apps"},
					Kinds:              []string{"Deployment"},
					Processes:          []string{"audit"},
				},
			},
			process:  Audit,
			obj:      makeObj("apps", "v1", "Deployment", "my-deploy", "default"),
			excluded: true,
		},
		{
			name: "apiGroup filter does not match",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"default"},
					APIGroups:          []string{"apps"},
					Kinds:              []string{"Deployment"},
					Processes:          []string{"audit"},
				},
			},
			process:  Audit,
			obj:      makeObj("", "v1", "Pod", "my-pod", "default"),
			excluded: false,
		},
		{
			name: "apiVersion filter matches",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"default"},
					APIVersions:        []string{"v1"},
					Kinds:              []string{"ConfigMap"},
					Processes:          []string{"*"},
				},
			},
			process:  Sync,
			obj:      makeObj("", "v1", "ConfigMap", "my-cm", "default"),
			excluded: true,
		},
		{
			name: "apiVersion filter does not match",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"default"},
					APIVersions:        []string{"v1beta1"},
					Kinds:              []string{"ConfigMap"},
					Processes:          []string{"*"},
				},
			},
			process:  Sync,
			obj:      makeObj("", "v1", "ConfigMap", "my-cm", "default"),
			excluded: false,
		},
		{
			name: "multiple kinds in filter",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"default"},
					Kinds:              []string{"ConfigMap", "Secret"},
					Processes:          []string{"*"},
				},
			},
			process:  Webhook,
			obj:      makeObj("", "v1", "Secret", "my-secret", "default"),
			excluded: true,
		},
		{
			name: "no GVK filter matches everything",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
					Processes:          []string{"*"},
				},
			},
			process:  Webhook,
			obj:      makeObj("apps", "v1", "Deployment", "my-deploy", "kube-system"),
			excluded: true,
		},
		{
			name: "GVK filter with wrong namespace not excluded",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
					Kinds:              []string{"ConfigMap"},
					Processes:          []string{"*"},
				},
			},
			process:  Audit,
			obj:      makeObj("", "v1", "ConfigMap", "my-cm", "default"),
			excluded: false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			excluder := New()
			if err := excluder.Add(tc.matchEntries); err != nil {
				t.Fatalf("unexpected error from Add: %v", err)
			}
			excluded, err := excluder.IsObjectExcluded(tc.process, tc.obj, tc.nsLabels)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if excluded != tc.excluded {
				t.Errorf("expected excluded=%v, got %v", tc.excluded, excluded)
			}
		})
	}
}

func TestIsObjectExcludedWithNamespaceSelector(t *testing.T) {
	tcs := []struct {
		name         string
		matchEntries []configv1alpha1.MatchEntry
		process      Process
		obj          *unstructured.Unstructured
		nsLabels     map[string]string
		excluded     bool
	}{
		{
			name: "namespace selector matches namespace object labels",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "dev"},
					},
					Processes: []string{"*"},
				},
			},
			process:  Webhook,
			obj:      makeNamespace("my-ns", map[string]string{"env": "dev"}),
			nsLabels: map[string]string{"env": "dev"},
			excluded: true,
		},
		{
			name: "namespace selector does not match namespace object labels",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "dev"},
					},
					Processes: []string{"*"},
				},
			},
			process:  Webhook,
			obj:      makeNamespace("my-ns", map[string]string{"env": "prod"}),
			nsLabels: map[string]string{"env": "prod"},
			excluded: false,
		},
		{
			name: "namespace selector with GVK filter - configmap in labeled namespace",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Kinds:       []string{"ConfigMap"},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "dev"},
					},
					Processes: []string{"*"},
				},
			},
			process:  Audit,
			obj:      makeObj("", "v1", "ConfigMap", "my-cm", "dev-ns"),
			nsLabels: map[string]string{"env": "dev"},
			excluded: true,
		},
		{
			name: "namespace selector with GVK filter - wrong kind not excluded",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Kinds:       []string{"ConfigMap"},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "dev"},
					},
					Processes: []string{"*"},
				},
			},
			process:  Audit,
			obj:      makeObj("", "v1", "Pod", "my-pod", "dev-ns"),
			nsLabels: map[string]string{"env": "dev"},
			excluded: false,
		},
		{
			name: "namespace selector with GVK filter - labels don't match",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Kinds:       []string{"ConfigMap"},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "dev"},
					},
					Processes: []string{"*"},
				},
			},
			process:  Audit,
			obj:      makeObj("", "v1", "ConfigMap", "my-cm", "prod-ns"),
			nsLabels: map[string]string{"env": "prod"},
			excluded: false,
		},
		{
			name: "namespace selector with nil nsLabels - not excluded",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "dev"},
					},
					Processes: []string{"*"},
				},
			},
			process:  Webhook,
			obj:      makeObj("", "v1", "Pod", "my-pod", "some-ns"),
			nsLabels: nil,
			excluded: false,
		},
		{
			name: "namespace selector via IsNamespaceExcluded on Namespace object",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "dev"},
					},
					Processes: []string{"*"},
				},
			},
			process:  Audit,
			obj:      makeNamespace("dev-ns", map[string]string{"env": "dev"}),
			nsLabels: map[string]string{"env": "dev"},
			excluded: true,
		},
		{
			name: "combined namespace pattern and selector",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"dev-*"},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "dev"},
					},
					Processes: []string{"*"},
				},
			},
			process:  Webhook,
			obj:      makeObj("", "v1", "Pod", "my-pod", "dev-ns"),
			nsLabels: map[string]string{"env": "dev"},
			excluded: true,
		},
		{
			name: "namespace pattern matches but selector does not",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"dev-*"},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "dev"},
					},
					Processes: []string{"*"},
				},
			},
			process:  Webhook,
			obj:      makeObj("", "v1", "Pod", "my-pod", "dev-ns"),
			nsLabels: map[string]string{"env": "prod"},
			excluded: false,
		},
		{
			name: "selector matches but namespace pattern does not",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"dev-*"},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "dev"},
					},
					Processes: []string{"*"},
				},
			},
			process:  Webhook,
			obj:      makeObj("", "v1", "Pod", "my-pod", "prod-ns"),
			nsLabels: map[string]string{"env": "dev"},
			excluded: false,
		},
		{
			name: "matchExpressions selector",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "env",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"dev", "staging"},
							},
						},
					},
					Processes: []string{"*"},
				},
			},
			process:  Webhook,
			obj:      makeObj("", "v1", "Pod", "my-pod", "staging-ns"),
			nsLabels: map[string]string{"env": "staging"},
			excluded: true,
		},
		{
			name: "matchExpressions selector does not match",
			matchEntries: []configv1alpha1.MatchEntry{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "env",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"dev", "staging"},
							},
						},
					},
					Processes: []string{"*"},
				},
			},
			process:  Webhook,
			obj:      makeObj("", "v1", "Pod", "my-pod", "prod-ns"),
			nsLabels: map[string]string{"env": "prod"},
			excluded: false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			excluder := New()
			if err := excluder.Add(tc.matchEntries); err != nil {
				t.Fatalf("unexpected error from Add: %v", err)
			}
			excluded, err := excluder.IsObjectExcluded(tc.process, tc.obj, tc.nsLabels)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if excluded != tc.excluded {
				t.Errorf("expected excluded=%v, got %v", tc.excluded, excluded)
			}
		})
	}
}

func TestIsNamespaceExcludedWithNamespaceSelector(t *testing.T) {
	// Test that IsNamespaceExcluded automatically uses the Namespace object's labels
	excluder := New()
	if err := excluder.Add([]configv1alpha1.MatchEntry{
		{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"env": "dev"},
			},
			Processes: []string{"*"},
		},
	}); err != nil {
		t.Fatalf("unexpected error from Add: %v", err)
	}

	// Namespace with matching labels - should be excluded
	devNS := makeNamespace("dev-ns", map[string]string{"env": "dev"})
	excluded, err := excluder.IsNamespaceExcluded(Webhook, devNS)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !excluded {
		t.Error("expected dev namespace to be excluded")
	}

	// Namespace without matching labels - should not be excluded
	prodNS := makeNamespace("prod-ns", map[string]string{"env": "prod"})
	excluded, err = excluder.IsNamespaceExcluded(Webhook, prodNS)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if excluded {
		t.Error("expected prod namespace to not be excluded")
	}

	// Non-namespace object - IsNamespaceExcluded can't provide ns labels, so selector won't match
	pod := makeObj("", "v1", "Pod", "my-pod", "dev-ns")
	excluded, err = excluder.IsNamespaceExcluded(Webhook, pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if excluded {
		t.Error("expected pod to not be excluded via IsNamespaceExcluded (no ns labels available)")
	}
}

func TestMultipleEntriesOR(t *testing.T) {
	// Multiple entries should be OR-ed: if any entry matches, the object is excluded
	excluder := New()
	if err := excluder.Add([]configv1alpha1.MatchEntry{
		{
			ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
			Processes:          []string{"*"},
		},
		{
			Kinds:     []string{"Secret"},
			Processes: []string{"*"},
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"sensitive": "true"},
			},
		},
	}); err != nil {
		t.Fatalf("unexpected error from Add: %v", err)
	}

	// Matches first entry (namespace pattern)
	pod := makeObj("", "v1", "Pod", "my-pod", "kube-system")
	excluded, err := excluder.IsObjectExcluded(Webhook, pod, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !excluded {
		t.Error("expected pod in kube-system to be excluded by first entry")
	}

	// Matches second entry (kind + namespace selector)
	secret := makeObj("", "v1", "Secret", "my-secret", "app-ns")
	excluded, err = excluder.IsObjectExcluded(Webhook, secret, map[string]string{"sensitive": "true"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !excluded {
		t.Error("expected secret in sensitive namespace to be excluded by second entry")
	}

	// Doesn't match either entry
	cm := makeObj("", "v1", "ConfigMap", "my-cm", "default")
	excluded, err = excluder.IsObjectExcluded(Webhook, cm, map[string]string{"sensitive": "false"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if excluded {
		t.Error("expected configmap in default to not be excluded")
	}
}

func TestEqualsAndReplace(t *testing.T) {
	e1 := New()
	if err := e1.Add([]configv1alpha1.MatchEntry{
		{
			ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
			Kinds:              []string{"ConfigMap"},
			Processes:          []string{"*"},
		},
	}); err != nil {
		t.Fatalf("unexpected error from Add: %v", err)
	}

	e2 := New()
	if err := e2.Add([]configv1alpha1.MatchEntry{
		{
			ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
			Kinds:              []string{"ConfigMap"},
			Processes:          []string{"*"},
		},
	}); err != nil {
		t.Fatalf("unexpected error from Add: %v", err)
	}

	if !e1.Equals(e2) {
		t.Error("expected equal excluders to be equal")
	}

	e3 := New()
	if err := e3.Add([]configv1alpha1.MatchEntry{
		{
			ExcludedNamespaces: []wildcard.Wildcard{"default"},
			Processes:          []string{"*"},
		},
	}); err != nil {
		t.Fatalf("unexpected error from Add: %v", err)
	}

	if e1.Equals(e3) {
		t.Error("expected different excluders to not be equal")
	}

	// Test Replace
	e1.Replace(e3)
	if !e1.Equals(e3) {
		t.Error("expected excluder to equal replacement after Replace")
	}
}

func TestEqualsForProcess(t *testing.T) {
	e1 := New()
	if err := e1.Add([]configv1alpha1.MatchEntry{
		{
			ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
			Processes:          []string{"audit"},
		},
		{
			ExcludedNamespaces: []wildcard.Wildcard{"default"},
			Processes:          []string{"webhook"},
		},
	}); err != nil {
		t.Fatalf("unexpected error from Add: %v", err)
	}

	e2 := New()
	if err := e2.Add([]configv1alpha1.MatchEntry{
		{
			ExcludedNamespaces: []wildcard.Wildcard{"kube-system"},
			Processes:          []string{"audit"},
		},
		{
			ExcludedNamespaces: []wildcard.Wildcard{"monitoring"},
			Processes:          []string{"webhook"},
		},
	}); err != nil {
		t.Fatalf("unexpected error from Add: %v", err)
	}

	if !e1.EqualsForProcess(Audit, e2) {
		t.Error("expected audit process entries to be equal")
	}

	if e1.EqualsForProcess(Webhook, e2) {
		t.Error("expected webhook process entries to not be equal")
	}
}
