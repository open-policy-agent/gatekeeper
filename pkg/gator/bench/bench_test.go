package bench

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clienterrors "github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
)

func TestRun_MissingInputs(t *testing.T) {
	_, err := Run(&Opts{
		Filenames:  []string{},
		Iterations: 10,
		Engine:     EngineRego,
	})
	if err == nil {
		t.Error("expected error for missing inputs")
	}
}

func TestRun_NoTemplates(t *testing.T) {
	// Create a temp file with just an object (no template)
	tmpDir := t.TempDir()
	objFile := filepath.Join(tmpDir, "object.yaml")
	err := os.WriteFile(objFile, []byte(`
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err = Run(&Opts{
		Filenames:  []string{tmpDir},
		Iterations: 1,
		Engine:     EngineRego,
	})
	if err == nil {
		t.Error("expected error for missing templates")
	}
}

func TestRun_Integration(t *testing.T) {
	// Create temp files with a template, constraint, and object
	tmpDir := t.TempDir()

	// Write template
	templateFile := filepath.Join(tmpDir, "template.yaml")
	err := os.WriteFile(templateFile, []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels
      validation:
        openAPIV3Schema:
          type: object
          properties:
            labels:
              type: array
              items:
                type: string
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequiredlabels
        violation[{"msg": msg}] {
          provided := {label | input.review.object.metadata.labels[label]}
          required := {label | label := input.parameters.labels[_]}
          missing := required - provided
          count(missing) > 0
          msg := sprintf("missing required labels: %v", [missing])
        }
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	// Write constraint
	constraintFile := filepath.Join(tmpDir, "constraint.yaml")
	err = os.WriteFile(constraintFile, []byte(`
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: require-team-label
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
  parameters:
    labels: ["team"]
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write constraint file: %v", err)
	}

	// Write object to review
	objectFile := filepath.Join(tmpDir, "pod.yaml")
	err = os.WriteFile(objectFile, []byte(`
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
spec:
  containers:
  - name: test
    image: nginx
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write object file: %v", err)
	}

	// Run benchmark with Rego engine
	results, err := Run(&Opts{
		Filenames:  []string{tmpDir},
		Iterations: 5,
		Warmup:     1,
		Engine:     EngineRego,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Engine != EngineRego {
		t.Errorf("expected engine %s, got %s", EngineRego, r.Engine)
	}
	if r.TemplateCount != 1 {
		t.Errorf("expected 1 template, got %d", r.TemplateCount)
	}
	if r.ConstraintCount != 1 {
		t.Errorf("expected 1 constraint, got %d", r.ConstraintCount)
	}
	if r.ObjectCount != 1 {
		t.Errorf("expected 1 object, got %d", r.ObjectCount)
	}
	if r.Iterations != 5 {
		t.Errorf("expected 5 iterations, got %d", r.Iterations)
	}
	// The pod is missing the required "team" label, so we expect violations
	if r.ViolationCount == 0 {
		t.Error("expected violations for missing label")
	}
	if r.ReviewsPerSecond <= 0 {
		t.Error("expected positive throughput")
	}
}

func TestRun_AllEngines(t *testing.T) {
	// Create temp files with a CEL-compatible template (using VAP code block)
	tmpDir := t.TempDir()

	// Write template with both Rego and CEL validation
	templateFile := filepath.Join(tmpDir, "template.yaml")
	err := os.WriteFile(templateFile, []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels
      validation:
        openAPIV3Schema:
          type: object
          properties:
            labels:
              type: array
              items:
                type: string
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequiredlabels
        violation[{"msg": msg}] {
          provided := {label | input.review.object.metadata.labels[label]}
          required := {label | label := input.parameters.labels[_]}
          missing := required - provided
          count(missing) > 0
          msg := sprintf("missing required labels: %v", [missing])
        }
      code:
        - engine: K8sNativeValidation
          source:
            validations:
              - expression: "has(object.metadata.labels) && object.metadata.labels.all(label, label in variables.params.labels)"
                message: "missing required labels"
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	// Write constraint
	constraintFile := filepath.Join(tmpDir, "constraint.yaml")
	err = os.WriteFile(constraintFile, []byte(`
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: require-team-label
spec:
  parameters:
    labels: ["team"]
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write constraint file: %v", err)
	}

	// Write object
	objectFile := filepath.Join(tmpDir, "pod.yaml")
	err = os.WriteFile(objectFile, []byte(`
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write object file: %v", err)
	}

	// Run with EngineAll
	results, err := Run(&Opts{
		Filenames:  []string{tmpDir},
		Iterations: 2,
		Warmup:     0,
		Engine:     EngineAll,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Should have results for both engines
	if len(results) != 2 {
		t.Fatalf("expected 2 results for EngineAll, got %d", len(results))
	}

	// First result should be Rego
	if results[0].Engine != EngineRego {
		t.Errorf("expected first result to be rego, got %s", results[0].Engine)
	}
	// Second result should be CEL
	if results[1].Engine != EngineCEL {
		t.Errorf("expected second result to be cel, got %s", results[1].Engine)
	}
}

func TestRun_NoConstraints(t *testing.T) {
	// Create a temp file with template but no constraint
	tmpDir := t.TempDir()

	// Write template
	templateFile := filepath.Join(tmpDir, "template.yaml")
	err := os.WriteFile(templateFile, []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequiredlabels
        violation[{"msg": msg}] {
          msg := "test"
        }
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	// Write object (no constraint)
	objectFile := filepath.Join(tmpDir, "pod.yaml")
	err = os.WriteFile(objectFile, []byte(`
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write object file: %v", err)
	}

	_, err = Run(&Opts{
		Filenames:  []string{tmpDir},
		Iterations: 1,
		Engine:     EngineRego,
	})
	if err == nil {
		t.Error("expected error for missing constraints")
	}
}

func TestRun_NoObjects(t *testing.T) {
	// Create a temp file with template and constraint but no objects
	tmpDir := t.TempDir()

	// Write template
	templateFile := filepath.Join(tmpDir, "template.yaml")
	err := os.WriteFile(templateFile, []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequiredlabels
        violation[{"msg": msg}] {
          msg := "test"
        }
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	// Write constraint only
	constraintFile := filepath.Join(tmpDir, "constraint.yaml")
	err = os.WriteFile(constraintFile, []byte(`
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: require-team-label
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write constraint file: %v", err)
	}

	_, err = Run(&Opts{
		Filenames:  []string{tmpDir},
		Iterations: 1,
		Engine:     EngineRego,
	})
	if err == nil {
		t.Error("expected error for missing objects to review")
	}
}

func TestRun_WithGatherStats(t *testing.T) {
	tmpDir := t.TempDir()

	// Write template
	templateFile := filepath.Join(tmpDir, "template.yaml")
	err := os.WriteFile(templateFile, []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequiredlabels
        violation[{"msg": msg}] {
          msg := "test"
        }
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	// Write constraint
	constraintFile := filepath.Join(tmpDir, "constraint.yaml")
	err = os.WriteFile(constraintFile, []byte(`
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: require-team-label
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write constraint file: %v", err)
	}

	// Write object
	objectFile := filepath.Join(tmpDir, "pod.yaml")
	err = os.WriteFile(objectFile, []byte(`
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write object file: %v", err)
	}

	// Run with GatherStats enabled
	results, err := Run(&Opts{
		Filenames:   []string{tmpDir},
		Iterations:  2,
		Warmup:      0,
		Engine:      EngineRego,
		GatherStats: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestRun_CELOnly(t *testing.T) {
	tmpDir := t.TempDir()

	// Write template with CEL code block
	templateFile := filepath.Join(tmpDir, "template.yaml")
	err := os.WriteFile(templateFile, []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels
  targets:
    - target: admission.k8s.gatekeeper.sh
      code:
        - engine: K8sNativeValidation
          source:
            validations:
              - expression: "true"
                message: "always pass"
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	// Write constraint
	constraintFile := filepath.Join(tmpDir, "constraint.yaml")
	err = os.WriteFile(constraintFile, []byte(`
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: require-team-label
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write constraint file: %v", err)
	}

	// Write object
	objectFile := filepath.Join(tmpDir, "pod.yaml")
	err = os.WriteFile(objectFile, []byte(`
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write object file: %v", err)
	}

	// Run with CEL engine only
	results, err := Run(&Opts{
		Filenames:  []string{tmpDir},
		Iterations: 2,
		Warmup:     0,
		Engine:     EngineCEL,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Engine != EngineCEL {
		t.Errorf("expected engine cel, got %s", results[0].Engine)
	}
}

func TestRun_SetupBreakdown(t *testing.T) {
	tmpDir := t.TempDir()

	// Write template
	templateFile := filepath.Join(tmpDir, "template.yaml")
	err := os.WriteFile(templateFile, []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequiredlabels
        violation[{"msg": msg}] {
          msg := "test"
        }
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	// Write constraint
	constraintFile := filepath.Join(tmpDir, "constraint.yaml")
	err = os.WriteFile(constraintFile, []byte(`
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: require-team-label
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write constraint file: %v", err)
	}

	// Write object
	objectFile := filepath.Join(tmpDir, "pod.yaml")
	err = os.WriteFile(objectFile, []byte(`
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write object file: %v", err)
	}

	results, err := Run(&Opts{
		Filenames:  []string{tmpDir},
		Iterations: 2,
		Warmup:     0,
		Engine:     EngineRego,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	// Check that setup breakdown fields are populated
	if r.SetupBreakdown.ClientCreation == 0 {
		t.Error("expected ClientCreation to be non-zero")
	}
	if r.SetupBreakdown.TemplateCompilation == 0 {
		t.Error("expected TemplateCompilation to be non-zero")
	}
	if r.SetupBreakdown.ConstraintLoading == 0 {
		t.Error("expected ConstraintLoading to be non-zero")
	}
	// DataLoading can be zero if there are no objects to load as data
}

func TestRun_SkippedTemplates(t *testing.T) {
	tmpDir := t.TempDir()

	// Write Rego-only template (incompatible with CEL)
	templateFile := filepath.Join(tmpDir, "template.yaml")
	err := os.WriteFile(templateFile, []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequiredlabels
        violation[{"msg": msg}] {
          msg := "test"
        }
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	// Write constraint
	constraintFile := filepath.Join(tmpDir, "constraint.yaml")
	err = os.WriteFile(constraintFile, []byte(`
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: require-team-label
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write constraint file: %v", err)
	}

	// Write object
	objectFile := filepath.Join(tmpDir, "pod.yaml")
	err = os.WriteFile(objectFile, []byte(`
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write object file: %v", err)
	}

	// Run with EngineAll - CEL should fail but Rego should succeed
	var buf bytes.Buffer
	results, err := Run(&Opts{
		Filenames:  []string{tmpDir},
		Iterations: 2,
		Warmup:     0,
		Engine:     EngineAll,
		Writer:     &buf,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Should have 1 result (only Rego succeeded)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Engine != EngineRego {
		t.Errorf("expected engine rego, got %s", results[0].Engine)
	}

	// Check that warning was written
	output := buf.String()
	if output == "" {
		t.Error("expected warning about skipped CEL engine")
	}
}

func TestIsEngineIncompatibleError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "ErrNoDriver directly",
			err:      clienterrors.ErrNoDriver,
			expected: true,
		},
		{
			name:     "ErrNoDriver wrapped",
			err:      fmt.Errorf("constraint template error: %w", clienterrors.ErrNoDriver),
			expected: true,
		},
		{
			name:     "ErrNoDriver double wrapped",
			err:      fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", clienterrors.ErrNoDriver)),
			expected: true,
		},
		{
			name:     "unrelated error",
			err:      &testError{msg: "some other error"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isEngineIncompatibleError(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestRun_CELWithGatherStats(t *testing.T) {
	tmpDir := t.TempDir()

	// Write template with CEL code block
	templateFile := filepath.Join(tmpDir, "template.yaml")
	err := os.WriteFile(templateFile, []byte(`
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels
  targets:
    - target: admission.k8s.gatekeeper.sh
      code:
        - engine: K8sNativeValidation
          source:
            validations:
              - expression: "true"
                message: "always pass"
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	// Write constraint
	constraintFile := filepath.Join(tmpDir, "constraint.yaml")
	err = os.WriteFile(constraintFile, []byte(`
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: require-team-label
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write constraint file: %v", err)
	}

	// Write object
	objectFile := filepath.Join(tmpDir, "pod.yaml")
	err = os.WriteFile(objectFile, []byte(`
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
`), 0o600)
	if err != nil {
		t.Fatalf("failed to write object file: %v", err)
	}

	// Run with CEL engine and GatherStats enabled
	results, err := Run(&Opts{
		Filenames:   []string{tmpDir},
		Iterations:  2,
		Warmup:      0,
		Engine:      EngineCEL,
		GatherStats: true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Engine != EngineCEL {
		t.Errorf("expected engine cel, got %s", results[0].Engine)
	}
}

func TestMakeClient_UnsupportedEngine(t *testing.T) {
	_, err := makeClient(Engine("invalid"), false)
	if err == nil {
		t.Error("expected error for unsupported engine")
	}
	if !strings.Contains(err.Error(), "unsupported engine") {
		t.Errorf("expected 'unsupported engine' error, got: %v", err)
	}
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
