package webhook

import (
	"context"
	"testing"

	"github.com/ghodss/yaml"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	atypes "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestAssignMetaValidation(t *testing.T) {
	tc := []struct {
		Name          string
		AssignMeta    string
		ErrorExpected bool
	}{
		{
			Name: "Valid Assign",
			AssignMeta: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: AssignMetadata
metadata:
  name: testAssignMeta
spec:
  location: metadata.labels.foo
  parameters:
    assign:
      value: bar
`,
			ErrorExpected: false,
		},
		{
			Name: "Invalid Path",
			AssignMeta: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: AssignMetadata
metadata:
  name: testAssignMeta
spec:
  location: metadata.foo.bar
  parameters:
    assign:
      value: bar
`,
			ErrorExpected: true,
		},
		{
			Name: "Invalid Assign",
			AssignMeta: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: AssignMetadata
metadata:
  name: testAssignMeta
spec:
  location: metadata.labels.bar
  parameters:
    assign:
      foo: bar
`,
			ErrorExpected: true,
		},
		{
			Name: "Assign not a string",
			AssignMeta: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: AssignMetadata
metadata:
  name: testAssignMeta
spec:
  location: metadata.labels.bar
  parameters:
    assign:
      value:
        foo: bar
`,
			ErrorExpected: true,
		},
		{
			Name: "Assign no value",
			AssignMeta: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: AssignMetadata
metadata:
  name: testAssignMeta
spec:
  location: metadata.labels.bar
  parameters:
    assign:
      zzz:
        foo: bar
`,
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			handler := validationHandler{webhookHandler: webhookHandler{}}
			b, err := yaml.YAMLToJSON([]byte(tt.AssignMeta))
			if err != nil {
				t.Fatalf("Error parsing yaml: %s", err)
			}
			review := &atypes.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "mutations.gatekeeper.sh",
						Version: "v1alpha1",
						Kind:    "AssignMetadata",
					},
					Object: runtime.RawExtension{
						Raw: b,
					},
				},
			}

			ctx := context.Background()
			_, err = handler.validateGatekeeperResources(ctx, review)
			if err != nil && !tt.ErrorExpected {
				t.Errorf("err = %s; want nil", err)
			}
			if err == nil && tt.ErrorExpected {
				t.Error("err = nil; want non-nil")
			}
		})
	}
}

func TestAssignValidation(t *testing.T) {
	tc := []struct {
		Name          string
		Assign        string
		ErrorExpected bool
	}{
		{
			Name: "Valid Assign",
			Assign: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: goodAssign
spec:
  applyTo:
    - groups: [""]
      versions: ["v1"]
      kinds: ["Pod"]
  location: "spec.containers[name:test].foo"
  parameters:
    assign:
      value:
        aa: bb
`,
			ErrorExpected: false,
		},
		{
			Name: "Changes Metadata",
			Assign: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: assignExample
spec:
  applyTo:
    - groups: [""]
      versions: ["v1"]
      kinds: ["Pod"]
  location: metadata.foo.bar
  parameters:
    assign:
      value: bar
`,
			ErrorExpected: true,
		},
		{
			Name: "No Value",
			Assign: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: assignExample
spec:
  applyTo:
  - groups: [""]
    versions: ["v1"]
    kinds: ["Pod"]
  location: spec.containers
  parameters:
    assign:
      zzz: bar
`,
			ErrorExpected: true,
		},
		{
			Name: "No Assign",
			Assign: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: assignExample
spec:
  applyTo:
  - groups: [""]
    versions: ["v1"]
    kinds: ["Pod"]
  location: spec.containers
`,
			ErrorExpected: true,
		},
		{
			Name: "Change the key",
			Assign: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: assignExample
spec:
  applyTo:
  - groups: [""]
    versions: ["v1"]
    kinds: ["Pod"]
  location: spec.containers[name:foo].name
  parameters:
    assign:
      value: bar
`,
			ErrorExpected: true,
		},
		{
			Name: "Assigning scalar as list item",
			Assign: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: assignExample
spec:
  applyTo:
  - groups: [""]
    versions: ["v1"]
    kinds: ["Pod"]
  location: spec.containers[name:foo]
  parameters:
    assign:
      value: xxx
`,
			ErrorExpected: true,
		},
		{
			Name: "Adding an object without the key",
			Assign: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: assignExample
spec:
  applyTo:
  - groups: [""]
    versions: ["v1"]
    kinds: ["Pod"]
  location: spec.containers[name:foo]
  parameters:
    assign:
      value:
        aa: bb
`,
			ErrorExpected: true,
		},
		{
			Name: "Adding an object changing the key",
			Assign: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: assignExample
spec:
  applyTo:
  - groups: [""]
    versions: ["v1"]
    kinds: ["Pod"]
  location: spec.containers[name:foo]
  parameters:
    assign:
      value:
        name: bar
`,
			ErrorExpected: true,
		},
		{
			Name: "Adding an object to a globbed list",
			Assign: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: assignExample
spec:
  applyTo:
    - groups: [""]
      versions: ["v1"]
      kinds: ["Pod"]
  location: spec.containers[name:*]
  parameters:
    assign:
      value:
        name: bar
`,
			ErrorExpected: true,
		},
		{
			Name: "Modifying a globbed list, no key",
			Assign: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: assignExample
spec:
  applyTo:
  - groups: [""]
    versions: ["v1"]
    kinds: ["Pod"]
  location: spec.containers[*].image
  parameters:
    assign:
      value:
        "my-image"
`,
			ErrorExpected: true,
		},
		{
			Name: "Modifying a globbed list, no key or glob",
			Assign: `
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: assignExample
spec:
  applyTo:
  - groups: [""]
    versions: ["v1"]
    kinds: ["Pod"]
  location: spec.containers[].image
  parameters:
    assign:
      value:
        "my-image"
`,
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			handler := validationHandler{webhookHandler: webhookHandler{}}
			b, err := yaml.YAMLToJSON([]byte(tt.Assign))
			if err != nil {
				t.Fatalf("Error parsing yaml: %s", err)
			}
			review := &atypes.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "mutations.gatekeeper.sh",
						Version: "v1alpha1",
						Kind:    "Assign",
					},
					Object: runtime.RawExtension{
						Raw: b,
					},
				},
			}

			ctx := context.Background()
			_, err = handler.validateGatekeeperResources(ctx, review)
			if err != nil && !tt.ErrorExpected {
				t.Errorf("%s: err = %s; want nil", tt.Name, err)
			}
			if err == nil && tt.ErrorExpected {
				t.Errorf("%s: err = nil; want non-nil", tt.Name)
			}
		})
	}
}
