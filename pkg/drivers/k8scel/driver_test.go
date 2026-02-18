package k8scel

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client/reviews"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	celSchema "github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// testReview implements the ARGetter interface for testing.
type testReview struct {
	request *admissionv1.AdmissionRequest
}

func (t *testReview) GetAdmissionRequest() *admissionv1.AdmissionRequest {
	return t.request
}

// TestMapToNamespace tests the conversion from map[string]interface{} to *corev1.Namespace.
func TestMapToNamespace(t *testing.T) {
	tests := []struct {
		name      string
		input     map[string]interface{}
		wantName  string
		wantLabel string
		wantErr   bool
	}{
		{
			name:    "nil input (cluster-scoped resource)",
			input:   nil,
			wantErr: false,
		},
		{
			name: "valid namespace with labels",
			input: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Namespace",
				"metadata": map[string]interface{}{
					"name": "test-namespace",
					"labels": map[string]interface{}{
						"environment": "production",
					},
				},
			},
			wantName:  "test-namespace",
			wantLabel: "production",
			wantErr:   false,
		},
		{
			name: "namespace without labels",
			input: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Namespace",
				"metadata": map[string]interface{}{
					"name": "simple-namespace",
				},
			},
			wantName: "simple-namespace",
			wantErr:  false,
		},
		{
			name: "invalid input causes marshal error",
			input: map[string]interface{}{
				"metadata": map[string]interface{}{
					// channels cannot be marshaled to JSON
					"invalid": make(chan int),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns, err := mapToNamespace(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("mapToNamespace() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			// If we expected an error, we're done checking
			if tt.wantErr {
				return
			}
			if tt.input == nil {
				if ns != nil {
					t.Errorf("mapToNamespace(nil) = %v, want nil", ns)
				}
				return
			}
			if ns == nil {
				t.Errorf("mapToNamespace() returned nil for non-nil input")
				return
			}
			if ns.Name != tt.wantName {
				t.Errorf("mapToNamespace().Name = %v, want %v", ns.Name, tt.wantName)
			}
			if tt.wantLabel != "" {
				if ns.Labels["environment"] != tt.wantLabel {
					t.Errorf("mapToNamespace().Labels[environment] = %v, want %v", ns.Labels["environment"], tt.wantLabel)
				}
			}
		})
	}
}

// TestDriverQueryWithNamespace tests that the CEL driver correctly handles namespace
// passed via ReviewCfg.Namespace for namespaceObject access in CEL expressions.
// Note: This is a basic test that verifies the namespace conversion and passing works.
// Full CEL namespaceObject integration is tested through E2E tests with real VAP.
func TestDriverQueryWithNamespace(t *testing.T) {
	// This test verifies that:
	// 1. mapToNamespace correctly converts namespace map to *corev1.Namespace
	// 2. The namespace is passed to the Query function without errors
	// Full CEL namespaceObject expression testing requires the full Kubernetes
	// CEL validation stack which is better tested via E2E tests.

	ctx := context.Background()

	driver, err := New()
	if err != nil {
		t.Fatalf("Failed to create driver: %v", err)
	}

	// Create a simple constraint template
	ct := makeSimpleTemplate()
	if err := driver.AddTemplate(ctx, ct); err != nil {
		t.Fatalf("Failed to add template: %v", err)
	}

	// Create a constraint
	constraint := makeSimpleConstraint("test-constraint")

	// Create a test pod
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-namespace",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "test", Image: "test:latest"},
			},
		},
	}

	podRaw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	if err != nil {
		t.Fatalf("Failed to convert pod: %v", err)
	}

	// Create admission request
	req := &testReview{
		request: &admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			Resource: metav1.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "pods",
			},
			Name:      "test-pod",
			Namespace: "test-namespace",
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: mustMarshal(t, podRaw)},
		},
	}

	tests := []struct {
		name      string
		namespace map[string]interface{}
	}{
		{
			name:      "cluster-scoped resource (no namespace)",
			namespace: nil,
		},
		{
			name: "namespaced resource with namespace labels",
			namespace: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Namespace",
				"metadata": map[string]interface{}{
					"name": "test-namespace",
					"labels": map[string]interface{}{
						"environment": "production",
					},
				},
			},
		},
		{
			name: "invalid namespace data (should log error but not fail)",
			namespace: map[string]interface{}{
				"metadata": map[string]interface{}{
					"invalid": make(chan int),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []reviews.ReviewOpt
			if tt.namespace != nil {
				opts = append(opts, reviews.Namespace(tt.namespace))
			}

			// Verify Query doesn't error when namespace is passed
			_, err := driver.Query(ctx, "SimpleCheck", []*unstructured.Unstructured{constraint}, req, opts...)
			if err != nil {
				t.Fatalf("Query failed: %v", err)
			}
		})
	}
}

// makeSimpleTemplate creates a simple CEL constraint template for testing.
func makeSimpleTemplate() *templates.ConstraintTemplate {
	ct := &templates.ConstraintTemplate{}
	ct.SetName("simplecheck")
	ct.Spec.CRD.Spec.Names.Kind = "SimpleCheck"

	ct.Spec.Targets = []templates.Target{{
		Target: "admission.k8s.gatekeeper.sh",
		Code: []templates.Code{{
			Engine: celSchema.Name,
			Source: &templates.Anything{
				Value: map[string]interface{}{
					"validations": []interface{}{
						map[string]interface{}{
							// Simple validation that always passes
							"expression": "true",
							"message":    "always passes",
						},
					},
					"failurePolicy": "Fail",
				},
			},
		}},
	}}

	return ct
}

// makeSimpleConstraint creates a constraint for the simple check template.
func makeSimpleConstraint(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "SimpleCheck",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"enforcementAction": "deny",
			},
		},
	}
}

func mustMarshal(t *testing.T, obj interface{}) []byte {
	t.Helper()
	jsonData, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("Failed to marshal to JSON: %v", err)
	}
	return jsonData
}
