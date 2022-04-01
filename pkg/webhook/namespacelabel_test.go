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
