package catalog

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDeriveK8sVersionRange(t *testing.T) {
	tests := []struct {
		name    string
		kinds   []groupKind
		wantMin string
		wantMax string
	}{
		{
			name:    "nil kinds",
			kinds:   nil,
			wantMin: "",
			wantMax: "",
		},
		{
			// networking.k8s.io Ingress was introduced in 1.14 and is still
			// served (stable v1), so only a lower bound is derived.
			name:    "stable resource yields min only",
			kinds:   []groupKind{{Group: "networking.k8s.io", Kind: "Ingress"}},
			wantMin: "v1.14.0",
			wantMax: "",
		},
		{
			// extensions Ingress was introduced in 1.1 and fully removed in 1.22,
			// so 1.21 is the last supported release.
			name:    "removed resource yields min and max",
			kinds:   []groupKind{{Group: "extensions", Kind: "Ingress"}},
			wantMin: "v1.1.0",
			wantMax: "v1.21.0",
		},
		{
			// extensions NetworkPolicy: introduced 1.3, removed 1.16.
			name:    "removed resource networkpolicy",
			kinds:   []groupKind{{Group: "extensions", Kind: "NetworkPolicy"}},
			wantMin: "v1.3.0",
			wantMax: "v1.15.0",
		},
		{
			// Resources present since v1.0 impose no meaningful lower bound.
			name:    "core resource introduced in 1.0 is suppressed",
			kinds:   []groupKind{{Group: "", Kind: "Pod"}},
			wantMin: "",
			wantMax: "",
		},
		{
			// min is the maximum introduced across targets; the suppressed Pod
			// bound does not lower it.
			name: "min is max across targets",
			kinds: []groupKind{
				{Group: "", Kind: "Pod"},
				{Group: "networking.k8s.io", Kind: "Ingress"},
			},
			wantMin: "v1.14.0",
			wantMax: "",
		},
		{
			// max is the minimum across removed targets; min is the max of
			// introductions.
			name: "max is min across removed targets",
			kinds: []groupKind{
				{Group: "extensions", Kind: "NetworkPolicy"}, // intro 1.3, removed 1.16
				{Group: "extensions", Kind: "Ingress"},       // intro 1.1, removed 1.22
			},
			wantMin: "v1.3.0",
			wantMax: "v1.15.0",
		},
		{
			// FlowSchema requires >=1.20 while NetworkPolicy is gone after 1.15:
			// no single cluster can satisfy both, so the range is discarded.
			name: "contradictory range discarded",
			kinds: []groupKind{
				{Group: "flowcontrol.apiserver.k8s.io", Kind: "FlowSchema"},
				{Group: "extensions", Kind: "NetworkPolicy"},
			},
			wantMin: "",
			wantMax: "",
		},
		{
			name:    "unknown custom resource yields nothing",
			kinds:   []groupKind{{Group: "example.com", Kind: "Widget"}},
			wantMin: "",
			wantMax: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMin, gotMax := deriveK8sVersionRange(tt.kinds)
			if gotMin != tt.wantMin {
				t.Errorf("min: got %q, want %q", gotMin, tt.wantMin)
			}
			if gotMax != tt.wantMax {
				t.Errorf("max: got %q, want %q", gotMax, tt.wantMax)
			}
		})
	}
}

func TestParseMatchKinds(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []groupKind
	}{
		{
			name: "cross product of groups and kinds",
			yaml: `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sTest
spec:
  match:
    kinds:
      - apiGroups: ["apps"]
        kinds: ["Deployment", "StatefulSet"]
`,
			want: []groupKind{
				{Group: "apps", Kind: "Deployment"},
				{Group: "apps", Kind: "StatefulSet"},
			},
		},
		{
			name: "empty apiGroups means core group",
			yaml: `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sTest
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
`,
			want: []groupKind{{Group: "", Kind: "Pod"}},
		},
		{
			name: "missing apiGroups defaults to core group",
			yaml: `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sTest
spec:
  match:
    kinds:
      - kinds: ["Pod"]
`,
			want: []groupKind{{Group: "", Kind: "Pod"}},
		},
		{
			name: "wildcards are skipped",
			yaml: `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sTest
spec:
  match:
    kinds:
      - apiGroups: ["*"]
        kinds: ["Pod"]
      - apiGroups: [""]
        kinds: ["*"]
      - apiGroups: ["apps"]
        kinds: ["Deployment"]
`,
			want: []groupKind{{Group: "apps", Kind: "Deployment"}},
		},
		{
			name: "no match block",
			yaml: `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sTest
spec: {}
`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMatchKinds([]byte(tt.yaml))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTargetGroupKinds(t *testing.T) {
	templateDir := t.TempDir()
	sampleDir := filepath.Join(templateDir, "samples", "example")
	if err := os.MkdirAll(sampleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	constraint := `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sTest
metadata:
  name: example
spec:
  match:
    kinds:
      - apiGroups: ["networking.k8s.io"]
        kinds: ["Ingress"]
`
	if err := os.WriteFile(filepath.Join(sampleDir, "constraint.yaml"), []byte(constraint), 0o600); err != nil {
		t.Fatal(err)
	}

	got := targetGroupKinds(templateDir)
	want := []groupKind{{Group: "networking.k8s.io", Kind: "Ingress"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// A template with no samples directory returns nothing rather than erroring.
	if got := targetGroupKinds(t.TempDir()); got != nil {
		t.Errorf("expected nil for missing samples dir, got %v", got)
	}
}

// TestParsePolicyFromTemplate_DerivesK8sVersion verifies the end-to-end fallback:
// a template without version annotations gets bounds derived from the resources
// its sample constraint targets, and explicit annotations take precedence.
func TestParsePolicyFromTemplate_DerivesK8sVersion(t *testing.T) {
	writeTemplate := func(t *testing.T, extraAnnotations, constraintGroup string) *Policy {
		t.Helper()
		root := t.TempDir()
		policyDir := filepath.Join(root, "library", "general", "versiontest")
		sampleDir := filepath.Join(policyDir, "samples", "example")
		if err := os.MkdirAll(sampleDir, 0o755); err != nil {
			t.Fatal(err)
		}

		template := `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8sversiontest
  annotations:
    description: "test"
    metadata.gatekeeper.sh/version: "1.0.0"
` + extraAnnotations + `
spec:
  crd:
    spec:
      names:
        kind: K8sVersionTest
`
		if err := os.WriteFile(filepath.Join(policyDir, "template.yaml"), []byte(template), 0o600); err != nil {
			t.Fatal(err)
		}

		constraint := `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sVersionTest
metadata:
  name: example
spec:
  match:
    kinds:
      - apiGroups: ["` + constraintGroup + `"]
        kinds: ["Ingress"]
`
		if err := os.WriteFile(filepath.Join(sampleDir, "constraint.yaml"), []byte(constraint), 0o600); err != nil {
			t.Fatal(err)
		}

		policy, err := parsePolicyFromTemplate(filepath.Join(policyDir, "template.yaml"), root)
		if err != nil {
			t.Fatalf("parsePolicyFromTemplate failed: %v", err)
		}
		return policy
	}

	t.Run("derives min from targeted resource", func(t *testing.T) {
		policy := writeTemplate(t, "", "networking.k8s.io")
		if policy.MinKubernetesVersion != "v1.14.0" {
			t.Errorf("MinKubernetesVersion: got %q, want %q", policy.MinKubernetesVersion, "v1.14.0")
		}
		if policy.MaxKubernetesVersion != "" {
			t.Errorf("MaxKubernetesVersion: got %q, want empty", policy.MaxKubernetesVersion)
		}
	})

	t.Run("derives min and max from removed resource", func(t *testing.T) {
		policy := writeTemplate(t, "", "extensions")
		if policy.MinKubernetesVersion != "v1.1.0" {
			t.Errorf("MinKubernetesVersion: got %q, want %q", policy.MinKubernetesVersion, "v1.1.0")
		}
		if policy.MaxKubernetesVersion != "v1.21.0" {
			t.Errorf("MaxKubernetesVersion: got %q, want %q", policy.MaxKubernetesVersion, "v1.21.0")
		}
	})

	t.Run("explicit annotation overrides derivation", func(t *testing.T) {
		annotations := `    metadata.gatekeeper.sh/minKubernetesVersion: "v1.25.0"`
		policy := writeTemplate(t, annotations, "networking.k8s.io")
		// Explicit min wins; max is still derived (none for a stable resource).
		if policy.MinKubernetesVersion != "v1.25.0" {
			t.Errorf("MinKubernetesVersion: got %q, want %q", policy.MinKubernetesVersion, "v1.25.0")
		}
		if policy.MaxKubernetesVersion != "" {
			t.Errorf("MaxKubernetesVersion: got %q, want empty", policy.MaxKubernetesVersion)
		}
	})
}
