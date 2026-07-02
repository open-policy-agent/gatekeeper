package util

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNamespaceToMap(t *testing.T) {
	tests := []struct {
		name       string
		namespace  *corev1.Namespace
		wantNil    bool
		wantErr    bool
		wantLabels map[string]string
		wantAnnos  map[string]string
		wantName   string
	}{
		{
			name:      "nil namespace returns nil",
			namespace: nil,
			wantNil:   true,
			wantErr:   false,
		},
		{
			name: "namespace with name only",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-namespace",
				},
			},
			wantNil:  false,
			wantErr:  false,
			wantName: "test-namespace",
		},
		{
			name: "namespace with labels",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "labeled-namespace",
					Labels: map[string]string{
						"environment": "production",
						"team":        "platform",
					},
				},
			},
			wantNil:  false,
			wantErr:  false,
			wantName: "labeled-namespace",
			wantLabels: map[string]string{
				"environment": "production",
				"team":        "platform",
			},
		},
		{
			name: "namespace with annotations",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "annotated-namespace",
					Annotations: map[string]string{
						"description": "Test namespace",
						"owner":       "admin@example.com",
					},
				},
			},
			wantNil:  false,
			wantErr:  false,
			wantName: "annotated-namespace",
			wantAnnos: map[string]string{
				"description": "Test namespace",
				"owner":       "admin@example.com",
			},
		},
		{
			name: "namespace with labels and annotations",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "full-namespace",
					Labels: map[string]string{
						"environment": "staging",
					},
					Annotations: map[string]string{
						"note": "test annotation",
					},
				},
			},
			wantNil:  false,
			wantErr:  false,
			wantName: "full-namespace",
			wantLabels: map[string]string{
				"environment": "staging",
			},
			wantAnnos: map[string]string{
				"note": "test annotation",
			},
		},
		{
			name: "namespace with empty labels and annotations",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "empty-metadata-namespace",
					Labels:      map[string]string{},
					Annotations: map[string]string{},
				},
			},
			wantNil:  false,
			wantErr:  false,
			wantName: "empty-metadata-namespace",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := NamespaceToMap(tc.namespace)

			if tc.wantErr && err == nil {
				t.Errorf("expected error but got nil")
				return
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tc.wantNil {
				if result != nil {
					t.Errorf("expected nil result but got %v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("expected non-nil result but got nil")
				return
			}

			metadata, ok := result["metadata"].(map[string]interface{})
			if !ok {
				t.Errorf("expected metadata to be map[string]interface{}, got %T", result["metadata"])
				return
			}

			if tc.wantName != "" {
				name, ok := metadata["name"].(string)
				if !ok {
					t.Errorf("expected name to be string, got %T", metadata["name"])
				} else if name != tc.wantName {
					t.Errorf("expected name %q, got %q", tc.wantName, name)
				}
			}

			if tc.wantLabels != nil {
				labels, ok := metadata["labels"].(map[string]interface{})
				if !ok {
					t.Errorf("expected labels to be map[string]interface{}, got %T", metadata["labels"])
				} else {
					for k, v := range tc.wantLabels {
						if labels[k] != v {
							t.Errorf("expected label %q=%q, got %q", k, v, labels[k])
						}
					}
				}
			}

			if tc.wantAnnos != nil {
				annos, ok := metadata["annotations"].(map[string]interface{})
				if !ok {
					t.Errorf("expected annotations to be map[string]interface{}, got %T", metadata["annotations"])
				} else {
					for k, v := range tc.wantAnnos {
						if annos[k] != v {
							t.Errorf("expected annotation %q=%q, got %q", k, v, annos[k])
						}
					}
				}
			}
		})
	}
}
