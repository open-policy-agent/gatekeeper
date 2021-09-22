package webhook

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/config/v1alpha1"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assign"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/assignmeta"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/schema"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	atypes "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestWebhookAssign(t *testing.T) {
	sys := mutation.NewSystem(mutation.SystemOpts{})

	v := &mutationsv1alpha1.Assign{
		ObjectMeta: metav1.ObjectMeta{Name: "AddFoo"},
		Spec: mutationsv1alpha1.AssignSpec{
			ApplyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Pod"}}},
			Location: "spec.value",
			Parameters: mutationsv1alpha1.Parameters{
				Assign: runtime.RawExtension{Raw: []byte(`{"value": "foo"}`)},
			},
		},
	}
	if err := assign.IsValidAssign(v); err != nil {
		t.Fatal(err)
	}

	m, err := mutators.MutatorForAssign(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := sys.Upsert(m); err != nil {
		t.Fatal(err)
	}

	h := &mutationHandler{
		webhookHandler: webhookHandler{
			injectedConfig: &configv1alpha1.Config{
				Spec: configv1alpha1.ConfigSpec{
					Validation: configv1alpha1.Validation{
						Traces: []configv1alpha1.Trace{},
					},
				},
			},
			client:          &nsGetter{},
			reader:          &nsGetter{},
			processExcluder: process.New(),
		},
		mutationSystem: sys,
		deserializer:   codecs.UniversalDeserializer(),
	}

	raw := []byte(`{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "acbd","namespace": "ns1"}}`)

	req := atypes.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			Object:    runtime.RawExtension{Raw: raw},
			Namespace: "ns1",
			Operation: admissionv1.Create,
		},
	}

	resp := h.Handle(context.Background(), req)

	expectedVal := []byte(`{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "acbd","namespace": "ns1"}, "spec": {"value": "foo"}}`)
	expected := admission.PatchResponseFromRaw(raw, expectedVal)

	if !reflect.DeepEqual(resp, expected) {
		t.Errorf("unexpected response: %+v\n\nexpected: %+v", resp, expected)
	}
}

func TestWebhookAssign_Conflict(t *testing.T) {
	sys := mutation.NewSystem(mutation.SystemOpts{})

	m1Assign := &mutationsv1alpha1.Assign{
		ObjectMeta: metav1.ObjectMeta{Name: "1"},
		Spec: mutationsv1alpha1.AssignSpec{
			ApplyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Pod"}}},
			Location: "spec.foo",
			Parameters: mutationsv1alpha1.Parameters{
				Assign: runtime.RawExtension{Raw: []byte(`{"value": "foo"}`)},
			},
		},
	}
	m1, err := mutators.MutatorForAssign(m1Assign)
	if err != nil {
		t.Fatal(err)
	}

	m2aAssign := &mutationsv1alpha1.Assign{
		ObjectMeta: metav1.ObjectMeta{Name: "2a"},
		Spec: mutationsv1alpha1.AssignSpec{
			ApplyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Pod"}}},
			Location: "spec.bar[name: foo].qux",
			Parameters: mutationsv1alpha1.Parameters{
				Assign: runtime.RawExtension{Raw: []byte(`{"value": "foo"}`)},
			},
		},
	}
	m2a, err := mutators.MutatorForAssign(m2aAssign)
	if err != nil {
		t.Fatal(err)
	}

	m2bAssign := &mutationsv1alpha1.Assign{
		ObjectMeta: metav1.ObjectMeta{Name: "2b"},
		Spec: mutationsv1alpha1.AssignSpec{
			ApplyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Pod"}}},
			Location: "spec.bar.qux",
			Parameters: mutationsv1alpha1.Parameters{
				Assign: runtime.RawExtension{Raw: []byte(`{"value": "foo"}`)},
			},
		},
	}
	m2b, err := mutators.MutatorForAssign(m2bAssign)
	if err != nil {
		t.Fatal(err)
	}

	err = sys.Upsert(m1)
	if err != nil {
		t.Fatal(err)
	}
	err = sys.Upsert(m2a)
	if err != nil {
		t.Fatal(err)
	}
	err = sys.Upsert(m2b)
	wantErr := schema.NewErrConflictingSchema(schema.IDSet{
		{Name: "2a"}: true, {Name: "2b"}: true,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v, want %v", err, wantErr)
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Pod"))

	mutated, err := sys.Mutate(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !mutated {
		t.Fatalf("got Mutate() = %t, want %t", mutated, true)
	}

	want := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"foo": "foo",
			},
		},
	}
	want.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Pod"))

	if diff := cmp.Diff(want, u); diff != "" {
		t.Error(diff)
	}

	// Fix conflict.
	err = sys.Remove(m2a.ID())
	if err != nil {
		t.Fatal(err)
	}

	mutated, err = sys.Mutate(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !mutated {
		t.Fatalf("got Mutate() = %t, want %t", mutated, true)
	}

	err = unstructured.SetNestedField(want.Object, "foo", "spec", "bar", "qux")
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(want, u); diff != "" {
		t.Error(diff)
	}
}

func TestWebhookAssignMetadata(t *testing.T) {
	sys := mutation.NewSystem(mutation.SystemOpts{})

	v := &mutationsv1alpha1.AssignMetadata{
		ObjectMeta: metav1.ObjectMeta{Name: "AddFoo"},
		Spec: mutationsv1alpha1.AssignMetadataSpec{
			Location: "metadata.labels.foo",
			Parameters: mutationsv1alpha1.MetadataParameters{
				Assign: runtime.RawExtension{Raw: []byte(`{"value": "bar"}`)},
			},
		},
	}
	if err := assignmeta.IsValidAssignMetadata(v); err != nil {
		t.Fatal(err)
	}

	m, err := mutators.MutatorForAssignMetadata(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := sys.Upsert(m); err != nil {
		t.Fatal(err)
	}

	h := &mutationHandler{
		webhookHandler: webhookHandler{
			injectedConfig: &configv1alpha1.Config{
				Spec: configv1alpha1.ConfigSpec{
					Validation: configv1alpha1.Validation{
						Traces: []configv1alpha1.Trace{},
					},
				},
			},
			client:          &nsGetter{},
			reader:          &nsGetter{},
			processExcluder: process.New(),
		},
		mutationSystem: sys,
		deserializer:   codecs.UniversalDeserializer(),
	}

	raw := []byte(`{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "acbd","namespace": "ns1"}}`)

	req := atypes.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			Object:    runtime.RawExtension{Raw: raw},
			Namespace: "ns1",
			Operation: admissionv1.Create,
		},
	}

	resp := h.Handle(context.Background(), req)

	expectedVal := []byte(`{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "acbd", "namespace": "ns1", "labels": {"foo":"bar"}}}`)
	expected := admission.PatchResponseFromRaw(raw, expectedVal)

	if !reflect.DeepEqual(resp, expected) {
		t.Errorf("unexpected response: %+v\n\nexpected: %+v", resp, expected)
	}
}
