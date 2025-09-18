package webhook

import (
	"context"
	"reflect"
	"testing"

	"github.com/open-policy-agent/cert-controller/pkg/rotator"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/assign"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/assignmeta"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func makeValue(v interface{}) mutationsunversioned.AssignField {
	return mutationsunversioned.AssignField{Value: &types.Anything{Value: v}}
}

func TestWebhookAssign(t *testing.T) {
	sys := mutation.NewSystem(mutation.SystemOpts{})

	v := &mutationsunversioned.Assign{
		ObjectMeta: metav1.ObjectMeta{Name: "AddFoo"},
		Spec: mutationsunversioned.AssignSpec{
			ApplyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"Pod"}}},
			Location: "spec.value",
			Parameters: mutationsunversioned.Parameters{
				Assign: makeValue("foo"),
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
		log:            log,
	}

	raw := []byte(`{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "acbd","namespace": "ns1"}}`)

	req := admission.Request{
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

func TestWebhookAssignMetadata(t *testing.T) {
	sys := mutation.NewSystem(mutation.SystemOpts{})

	v := &mutationsunversioned.AssignMetadata{
		ObjectMeta: metav1.ObjectMeta{Name: "AddFoo"},
		Spec: mutationsunversioned.AssignMetadataSpec{
			Location: "metadata.labels.foo",
			Parameters: mutationsunversioned.MetadataParameters{
				Assign: makeValue("bar"),
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
		log:            log,
	}

	raw := []byte(`{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "acbd","namespace": "ns1"}}`)

	req := admission.Request{
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

func TestAppendMutationWebhookIfEnabled(t *testing.T) {
	tests := []struct {
		name               string
		mwhName            string
		additionalMwhNames string
		input              []rotator.WebhookInfo
		expectedWebhooks   []rotator.WebhookInfo
	}{
		{
			name:               "adding to empty list",
			mwhName:            "test-mutation-webhook",
			additionalMwhNames: "additional-mutation-webhook-1,additional-mutation-webhook-2",
			input:              []rotator.WebhookInfo{},
			expectedWebhooks: []rotator.WebhookInfo{
				{Name: "test-mutation-webhook", Type: rotator.Mutating},
				{Name: "additional-mutation-webhook-1", Type: rotator.Mutating},
				{Name: "additional-mutation-webhook-2", Type: rotator.Mutating},
			},
		},
		{
			name:               "adding only one webhook",
			mwhName:            "test-mutation-webhook",
			additionalMwhNames: "",
			input:              []rotator.WebhookInfo{},
			expectedWebhooks: []rotator.WebhookInfo{
				{Name: "test-mutation-webhook", Type: rotator.Mutating},
			},
		},
		{
			name:               "adding to existing webhooks",
			mwhName:            "test-mutation-webhook",
			additionalMwhNames: "additional-mutation-webhook-1,additional-mutation-webhook-2",
			input: []rotator.WebhookInfo{
				{Name: "existing-webhook"},
			},
			expectedWebhooks: []rotator.WebhookInfo{
				{Name: "existing-webhook"},
				{Name: "test-mutation-webhook", Type: rotator.Mutating},
				{Name: "additional-mutation-webhook-1", Type: rotator.Mutating},
				{Name: "additional-mutation-webhook-2", Type: rotator.Mutating},
			},
		},
		{
			name:               "deduplicate mwhName and additionalMwhNames",
			mwhName:            "test-mutation-webhook",
			additionalMwhNames: "test-mutation-webhook,additional-mutation-webhook-1,additional-mutation-webhook-2",
			input: []rotator.WebhookInfo{
				{Name: "existing-webhook"},
			},
			expectedWebhooks: []rotator.WebhookInfo{
				{Name: "existing-webhook"},
				{Name: "test-mutation-webhook", Type: rotator.Mutating},
				{Name: "additional-mutation-webhook-1", Type: rotator.Mutating},
				{Name: "additional-mutation-webhook-2", Type: rotator.Mutating},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			MwhName = &tt.mwhName
			AdditionalMwhNamesToRotateCerts = &tt.additionalMwhNames
			actualWebhooks := AppendMutationWebhookIfEnabled(tt.input)
			require.Equal(t, tt.expectedWebhooks, actualWebhooks)
		})
	}
}
