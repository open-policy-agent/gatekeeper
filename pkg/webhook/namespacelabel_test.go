package webhook

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func gvk(group, version, kind string) metav1.GroupVersionKind {
	return metav1.GroupVersionKind{Group: group, Version: version, Kind: kind}
}

func TestAdmission(t *testing.T) {
	tests := []struct {
		name          string
		kind          metav1.GroupVersionKind
		obj           client.Object
		op            admissionv1.Operation
		expectAllowed bool
	}{
		{
			name:          "Wrong group",
			kind:          gvk("random", "v1", "Namespace"),
			obj:           &unstructured.Unstructured{},
			op:            admissionv1.Create,
			expectAllowed: true,
		},
		{
			name:          "Wrong kind",
			kind:          gvk("", "v1", "Arbitrary"),
			obj:           &unstructured.Unstructured{},
			op:            admissionv1.Create,
			expectAllowed: true,
		},
		{
			name: "Bad Namespace create rejected",
			kind: gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "random-namespace",
					Labels: map[string]string{ignoreLabel: "true"},
				},
			},
			op:            admissionv1.Create,
			expectAllowed: false,
		},
		{
			name: "Bad Namespace update rejected",
			kind: gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "random-namespace",
					Labels: map[string]string{ignoreLabel: "true"},
				},
			},
			op:            admissionv1.Update,
			expectAllowed: false,
		},
		{
			name: "Bad Namespace delete allowed",
			kind: gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "random-namespace",
					Labels: map[string]string{ignoreLabel: "true"},
				},
			},
			op:            admissionv1.Delete,
			expectAllowed: true,
		},
		{
			name: "Bad Namespace no label allowed",
			kind: gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "random-namespace",
				},
			},
			op:            admissionv1.Create,
			expectAllowed: true,
		},
		{
			name: "Bad Namespace irrelevant label allowed",
			kind: gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "random-namespace",
					Labels: map[string]string{"some-label": "true"},
				},
			},
			op:            admissionv1.Update,
			expectAllowed: true,
		},
		{
			name: "Exempt Namespace create allowed",
			kind: gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "random-allowed-ns",
					Labels: map[string]string{ignoreLabel: "true"},
				},
			},
			op:            admissionv1.Create,
			expectAllowed: true,
		},
		{
			name: "Exempt Namespace update allowed",
			kind: gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "random-allowed-ns",
					Labels: map[string]string{ignoreLabel: "true"},
				},
			},
			op:            admissionv1.Update,
			expectAllowed: true,
		},
		{
			name: "Exempt Namespace delete allowed",
			kind: gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "random-allowed-ns",
					Labels: map[string]string{ignoreLabel: "true"},
				},
			},
			op:            admissionv1.Delete,
			expectAllowed: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() {
				exemptNamespace = nil
			})

			exemptNamespace = map[string]bool{"random-allowed-ns": true}
			gvk := tt.obj.GetObjectKind()
			gvk.SetGroupVersionKind(schema.GroupVersionKind{Group: tt.kind.Group, Version: tt.kind.Version, Kind: tt.kind.Kind})
			bytes, err := json.Marshal(tt.obj)
			if err != nil {
				t.Fatal(err)
			}
			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind:      tt.kind,
					Object:    runtime.RawExtension{Raw: bytes},
					Operation: tt.op,
				},
			}
			handler := &namespaceLabelHandler{}
			resp := handler.Handle(context.Background(), req)
			if resp.Allowed != tt.expectAllowed {
				t.Errorf("resp.Allowed = %v, expected %v. Reason: %s", resp.Allowed, tt.expectAllowed, resp.Result.Reason)
			}
		})
	}
}

func TestAdmissionPrefix(t *testing.T) {
	tests := []struct {
		name          string
		prefixes      []string
		kind          metav1.GroupVersionKind
		obj           client.Object
		op            admissionv1.Operation
		expectAllowed bool
	}{
		{
			name:     "Exempt Namespace create allowed",
			prefixes: []string{"random-"},
			kind:     gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "random-allowed-ns",
					Labels: map[string]string{ignoreLabel: "true"},
				},
			},
			op:            admissionv1.Create,
			expectAllowed: true,
		},
		{
			name:     "Exempt Namespace update allowed",
			prefixes: []string{"random-"},
			kind:     gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "random-allowed-ns",
					Labels: map[string]string{ignoreLabel: "true"},
				},
			},
			op:            admissionv1.Update,
			expectAllowed: true,
		},
		{
			name:     "Exempt Namespace delete allowed",
			prefixes: []string{"random-"},
			kind:     gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "random-allowed-ns",
					Labels: map[string]string{ignoreLabel: "true"},
				},
			},
			op:            admissionv1.Delete,
			expectAllowed: true,
		},
		{
			name:     "Bad Namespace create rejected",
			prefixes: []string{"random-"},
			kind:     gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "wrongprefix-random-namespace",
					Labels: map[string]string{ignoreLabel: "true"},
				},
			},
			op:            admissionv1.Create,
			expectAllowed: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() {
				exemptNamespacePrefix = nil
			})

			exemptNamespacePrefix = map[string]bool{}
			for _, p := range tt.prefixes {
				exemptNamespacePrefix[p] = true
			}
			gvk := tt.obj.GetObjectKind()
			gvk.SetGroupVersionKind(schema.GroupVersionKind{Group: tt.kind.Group, Version: tt.kind.Version, Kind: tt.kind.Kind})
			bytes, err := json.Marshal(tt.obj)
			if err != nil {
				t.Fatal(err)
			}
			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind:      tt.kind,
					Object:    runtime.RawExtension{Raw: bytes},
					Operation: tt.op,
				},
			}
			handler := &namespaceLabelHandler{}
			resp := handler.Handle(context.Background(), req)
			if resp.Allowed != tt.expectAllowed {
				t.Errorf("resp.Allowed = %v, expected %v. Reason: %s", resp.Allowed, tt.expectAllowed, resp.Result.Reason)
			}
		})
	}
}

func TestAdmissionSuffix(t *testing.T) {
	tests := []struct {
		name          string
		suffixes      []string
		kind          metav1.GroupVersionKind
		obj           client.Object
		op            admissionv1.Operation
		expectAllowed bool
	}{
		{
			name:     "Exempt Namespace create allowed",
			suffixes: []string{"-random"},
			kind:     gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "allowed-ns-random",
					Labels: map[string]string{ignoreLabel: "true"},
				},
			},
			op:            admissionv1.Create,
			expectAllowed: true,
		},
		{
			name:     "Exempt Namespace update allowed",
			suffixes: []string{"-random"},
			kind:     gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "allowed-ns-random",
					Labels: map[string]string{ignoreLabel: "true"},
				},
			},
			op:            admissionv1.Update,
			expectAllowed: true,
		},
		{
			name:     "Exempt Namespace delete allowed",
			suffixes: []string{"-random"},
			kind:     gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "allowed-ns-random",
					Labels: map[string]string{ignoreLabel: "true"},
				},
			},
			op:            admissionv1.Delete,
			expectAllowed: true,
		},
		{
			name:     "Bad Namespace create rejected",
			suffixes: []string{"-random"},
			kind:     gvk("", "v1", "Namespace"),
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "random-namespace-wrongsuffix",
					Labels: map[string]string{ignoreLabel: "true"},
				},
			},
			op:            admissionv1.Create,
			expectAllowed: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() {
				exemptNamespaceSuffix = nil
			})

			exemptNamespaceSuffix = map[string]bool{}
			for _, p := range tt.suffixes {
				exemptNamespaceSuffix[p] = true
			}
			gvk := tt.obj.GetObjectKind()
			gvk.SetGroupVersionKind(schema.GroupVersionKind{Group: tt.kind.Group, Version: tt.kind.Version, Kind: tt.kind.Kind})
			bytes, err := json.Marshal(tt.obj)
			if err != nil {
				t.Fatal(err)
			}
			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind:      tt.kind,
					Object:    runtime.RawExtension{Raw: bytes},
					Operation: tt.op,
				},
			}
			handler := &namespaceLabelHandler{}
			resp := handler.Handle(context.Background(), req)
			if resp.Allowed != tt.expectAllowed {
				t.Errorf("resp.Allowed = %v, expected %v. Reason: %s", resp.Allowed, tt.expectAllowed, resp.Result.Reason)
			}
		})
	}
}

func TestBadSerialization(t *testing.T) {
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Kind:      gvk("", "v1", "Namespace"),
			Object:    runtime.RawExtension{Raw: []byte("asdfadsfa  awdf+-=-=pasdf")},
			Operation: admissionv1.Create,
		},
	}
	handler := &namespaceLabelHandler{}
	resp := handler.Handle(context.Background(), req)
	if resp.Allowed {
		t.Errorf("resp.Allowed = %v, expected false. Reason: %s", resp.Allowed, resp.Result.Reason)
	}
}

func TestGetAllExemptedNamespacesWithWildcard(t *testing.T) {
	tests := []struct {
		name                  string
		exemptNamespaces      map[string]bool
		exemptPrefixes        map[string]bool
		exemptSuffixes        map[string]bool
		expectedNamespaces    []string
		expectedPrefixPattern []string
		expectedSuffixPattern []string
	}{
		{
			name:                  "empty exemptions",
			exemptNamespaces:      map[string]bool{},
			exemptPrefixes:        map[string]bool{},
			exemptSuffixes:        map[string]bool{},
			expectedNamespaces:    []string{},
			expectedPrefixPattern: []string{},
			expectedSuffixPattern: []string{},
		},
		{
			name:                  "only exact match namespaces",
			exemptNamespaces:      map[string]bool{"kube-system": true, "gatekeeper-system": true},
			exemptPrefixes:        map[string]bool{},
			exemptSuffixes:        map[string]bool{},
			expectedNamespaces:    []string{"kube-system", "gatekeeper-system"},
			expectedPrefixPattern: []string{},
			expectedSuffixPattern: []string{},
		},
		{
			name:                  "only prefix match namespaces",
			exemptNamespaces:      map[string]bool{},
			exemptPrefixes:        map[string]bool{"kube-": true, "openshift-": true},
			exemptSuffixes:        map[string]bool{},
			expectedNamespaces:    []string{},
			expectedPrefixPattern: []string{"kube-*", "openshift-*"},
			expectedSuffixPattern: []string{},
		},
		{
			name:                  "only suffix match namespaces",
			exemptNamespaces:      map[string]bool{},
			exemptPrefixes:        map[string]bool{},
			exemptSuffixes:        map[string]bool{"-system": true, "-monitoring": true},
			expectedNamespaces:    []string{},
			expectedPrefixPattern: []string{},
			expectedSuffixPattern: []string{"*-system", "*-monitoring"},
		},
		{
			name:                  "mixed exemptions",
			exemptNamespaces:      map[string]bool{"default": true, "kube-system": true},
			exemptPrefixes:        map[string]bool{"kube-": true},
			exemptSuffixes:        map[string]bool{"-system": true},
			expectedNamespaces:    []string{"default", "kube-system"},
			expectedPrefixPattern: []string{"kube-*"},
			expectedSuffixPattern: []string{"*-system"},
		},
		{
			name:                  "single values",
			exemptNamespaces:      map[string]bool{"test-ns": true},
			exemptPrefixes:        map[string]bool{"dev-": true},
			exemptSuffixes:        map[string]bool{"-prod": true},
			expectedNamespaces:    []string{"test-ns"},
			expectedPrefixPattern: []string{"dev-*"},
			expectedSuffixPattern: []string{"*-prod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original values
			origExemptNamespace := exemptNamespace
			origExemptNamespacePrefix := exemptNamespacePrefix
			origExemptNamespaceSuffix := exemptNamespaceSuffix

			// Restore original values after test
			defer func() {
				exemptNamespace = origExemptNamespace
				exemptNamespacePrefix = origExemptNamespacePrefix
				exemptNamespaceSuffix = origExemptNamespaceSuffix
			}()

			// Set test values
			exemptNamespace = tt.exemptNamespaces
			exemptNamespacePrefix = tt.exemptPrefixes
			exemptNamespaceSuffix = tt.exemptSuffixes

			// Call the function
			result := GetAllExemptedNamespacesWithWildcard()

			// Calculate expected total
			expectedTotal := len(tt.expectedNamespaces) + len(tt.expectedPrefixPattern) + len(tt.expectedSuffixPattern)

			// Verify total count
			if len(result) != expectedTotal {
				t.Errorf("GetAllExemptedNamespacesWithWildcard() returned %d items, expected %d. Got: %v",
					len(result), expectedTotal, result)
			}

			// Create a map for easier lookup
			resultMap := make(map[string]bool)
			for _, ns := range result {
				resultMap[ns] = true
			}

			// Verify exact match namespaces
			for _, expected := range tt.expectedNamespaces {
				if !resultMap[expected] {
					t.Errorf("Expected namespace %q not found in result: %v", expected, result)
				}
			}

			// Verify prefix patterns (should have * suffix)
			for _, expected := range tt.expectedPrefixPattern {
				if !resultMap[expected] {
					t.Errorf("Expected prefix pattern %q not found in result: %v", expected, result)
				}
			}

			// Verify suffix patterns (should have * prefix)
			for _, expected := range tt.expectedSuffixPattern {
				if !resultMap[expected] {
					t.Errorf("Expected suffix pattern %q not found in result: %v", expected, result)
				}
			}
		})
	}
}
