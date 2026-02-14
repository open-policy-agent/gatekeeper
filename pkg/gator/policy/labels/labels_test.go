package labels

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestAddManagedLabels(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "templates.gatekeeper.sh/v1",
			"kind":       "ConstraintTemplate",
			"metadata": map[string]interface{}{
				"name": "test",
			},
		},
	}

	AddManagedLabels(obj, "v1.2.3", "test-bundle", "https://github.com/open-policy-agent/gatekeeper-library")

	labels := obj.GetLabels()
	assert.Equal(t, ManagedByValue, labels[LabelManagedBy])
	assert.Equal(t, "test-bundle", labels[LabelBundle])

	annotations := obj.GetAnnotations()
	assert.Equal(t, "v1.2.3", annotations[AnnotationVersion])
	assert.Equal(t, "https://github.com/open-policy-agent/gatekeeper-library", annotations[AnnotationSource])
	assert.NotEmpty(t, annotations[AnnotationInstalledAt])

	// Verify installedAt is valid time
	_, err := time.Parse(time.RFC3339, annotations[AnnotationInstalledAt])
	assert.NoError(t, err)
}

func TestAddManagedLabels_WithoutBundle(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "templates.gatekeeper.sh/v1",
			"kind":       "ConstraintTemplate",
			"metadata": map[string]interface{}{
				"name": "test",
			},
		},
	}

	AddManagedLabels(obj, "v1.0.0", "", "test-source")

	labels := obj.GetLabels()
	assert.Equal(t, ManagedByValue, labels[LabelManagedBy])
	_, hasBundle := labels[LabelBundle]
	assert.False(t, hasBundle)
}

func TestAddManagedLabels_PreservesExistingLabels(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "templates.gatekeeper.sh/v1",
			"kind":       "ConstraintTemplate",
			"metadata": map[string]interface{}{
				"name": "test",
				"labels": map[string]interface{}{
					"existing-label": "existing-value",
				},
			},
		},
	}

	AddManagedLabels(obj, "v1.0.0", "", "test-source")

	labels := obj.GetLabels()
	assert.Equal(t, "existing-value", labels["existing-label"])
	assert.Equal(t, ManagedByValue, labels[LabelManagedBy])
}

func TestIsManagedByGator(t *testing.T) {
	tests := []struct {
		name        string
		labels      map[string]string
		annotations map[string]string
		expected    bool
	}{
		{
			name:        "managed by gator (both label and annotation)",
			labels:      map[string]string{LabelManagedBy: ManagedByValue},
			annotations: map[string]string{AnnotationSource: "gatekeeper-library"},
			expected:    true,
		},
		{
			name:        "label only (missing annotation)",
			labels:      map[string]string{LabelManagedBy: ManagedByValue},
			annotations: nil,
			expected:    false,
		},
		{
			name:        "annotation only (missing label)",
			labels:      nil,
			annotations: map[string]string{AnnotationSource: "gatekeeper-library"},
			expected:    false,
		},
		{
			name:        "managed by other",
			labels:      map[string]string{LabelManagedBy: "helm"},
			annotations: map[string]string{AnnotationSource: "gatekeeper-library"},
			expected:    false,
		},
		{
			name:        "no managed-by label",
			labels:      map[string]string{"other": "value"},
			annotations: nil,
			expected:    false,
		},
		{
			name:        "no labels or annotations",
			labels:      nil,
			annotations: nil,
			expected:    false,
		},
		{
			name:        "empty annotation value",
			labels:      map[string]string{LabelManagedBy: ManagedByValue},
			annotations: map[string]string{AnnotationSource: ""},
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "test",
					},
				},
			}
			if tt.labels != nil {
				obj.SetLabels(tt.labels)
			}
			if tt.annotations != nil {
				obj.SetAnnotations(tt.annotations)
			}

			result := IsManagedByGator(obj)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetPolicyVersion(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name": "test",
				"annotations": map[string]interface{}{
					AnnotationVersion: "v1.2.3",
				},
			},
		},
	}

	version := GetPolicyVersion(obj)
	assert.Equal(t, "v1.2.3", version)

	// Without annotations
	obj2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name": "test",
			},
		},
	}
	version = GetPolicyVersion(obj2)
	assert.Empty(t, version)
}

func TestGetBundle(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name": "test",
				"labels": map[string]interface{}{
					LabelBundle: "pod-security-baseline",
				},
			},
		},
	}

	bundle := GetBundle(obj)
	assert.Equal(t, "pod-security-baseline", bundle)

	// Without labels
	obj2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name": "test",
			},
		},
	}
	bundle = GetBundle(obj2)
	assert.Empty(t, bundle)
}
