package webhook

import (
	"context"
	"testing"

	"github.com/ghodss/yaml"
	templv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/gatekeeper/pkg/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	atypes "sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
)

const (
	bad_rego_template = `
apiVersion: templates.gatekeeper.sh/v1beta1
kind: ConstraintTemplate
metadata:
  name: k8sbadrego
spec:
  crd:
    spec:
      names:
        kind: K8sBadRego
        listKind: K8sBadRegoList
        plural: k8sbadrego
        singular: k8sbadrego
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package badrego

        violation[{"msg": msg}] {
        msg := "I'm sure this will work"
`

	good_rego_template = `
apiVersion: templates.gatekeeper.sh/v1beta1
kind: ConstraintTemplate
metadata:
  name: k8sgoodrego
spec:
  crd:
    spec:
      names:
        kind: K8sGoodRego
        listKind: K8sGoodRegoList
        plural: k8sgoodrego
        singular: k8sgoodrego
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package goodrego

        violation[{"msg": msg}] {
          msg := "Maybe this will work?"
        }
`

	bad_labelselector = `
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sGoodRego
metadata:
  name: bad-labelselector
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Namespace"]
    labelSelector:
      matchExpressions:
        - operator: "In"
          key: "something"
`

	good_labelselector = `
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sGoodRego
metadata:
  name: good-labelselector
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Namespace"]
    labelSelector:
      matchExpressions:
        - operator: "In"
          key: "something"
          values: ["anything"]
`

	bad_namespaceselector = `
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sGoodRego
metadata:
  name: bad-namespaceselector
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
    namespaceSelector:
      matchExpressions:
        - operator: "In"
          key: "something"
`

	good_namespaceselector = `
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sGoodRego
metadata:
  name: good-namespaceselector
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
    namespaceSelector:
      matchExpressions:
        - operator: "In"
          key: "something"
          values: ["anything"]
`

	good_enforcementaction = `
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sGoodRego
metadata:
  name: good-namespaceselector
spec:
  enforcementAction: dryrun
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
`

	bad_enforcementaction = `
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sGoodRego
metadata:
  name: bad-namespaceselector
spec:
  enforcementAction: test
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
`
)

func makeOpaClient() (client.Client, error) {
	target := &target.K8sValidationTarget{}
	driver := local.New(local.Tracing(false))
	backend, err := client.NewBackend(client.Driver(driver))
	if err != nil {
		return nil, err
	}
	c, err := backend.NewClient(client.Targets(target))
	if err != nil {
		return nil, err
	}
	return c, nil
}

func TestTemplateValidation(t *testing.T) {
	tc := []struct {
		Name          string
		Template      string
		ErrorExpected bool
	}{
		{
			Name:          "Valid Template",
			Template:      good_rego_template,
			ErrorExpected: false,
		},
		{
			Name:          "Invalid Template",
			Template:      bad_rego_template,
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			opa, err := makeOpaClient()
			if err != nil {
				t.Fatalf("Could not initialize OPA: %s", err)
			}
			handler := validationHandler{opa: opa}
			b, err := yaml.YAMLToJSON([]byte(tt.Template))
			if err != nil {
				t.Fatalf("Error parsing yaml: %s", err)
			}
			review := atypes.Request{
				AdmissionRequest: &admissionv1beta1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "templates.gatekeeper.sh",
						Version: "v1beta1",
						Kind:    "ConstraintTemplate",
					},
					Object: runtime.RawExtension{
						Raw: b,
					},
				},
			}
			_, err = handler.validateGatekeeperResources(context.Background(), review)
			if err != nil && !tt.ErrorExpected {
				t.Errorf("err = %s; want nil", err)
			}
			if err == nil && tt.ErrorExpected {
				t.Error("err = nil; want non-nil")
			}
		})
	}
}

func TestConstraintValidation(t *testing.T) {
	tc := []struct {
		Name          string
		Template      string
		Constraint    string
		ErrorExpected bool
	}{
		{
			Name:          "Valid Constraint labelselector",
			Template:      good_rego_template,
			Constraint:    good_labelselector,
			ErrorExpected: false,
		},
		{
			Name:          "Invalid Constraint labelselector",
			Template:      good_rego_template,
			Constraint:    bad_labelselector,
			ErrorExpected: true,
		},
		{
			Name:          "Valid Constraint namespaceselector",
			Template:      good_rego_template,
			Constraint:    good_namespaceselector,
			ErrorExpected: false,
		},
		{
			Name:          "Invalid Constraint namespaceselector",
			Template:      good_rego_template,
			Constraint:    bad_namespaceselector,
			ErrorExpected: true,
		},
		{
			Name:          "Valid Constraint enforcementaction",
			Template:      good_rego_template,
			Constraint:    good_enforcementaction,
			ErrorExpected: false,
		},
		{
			Name:          "Invalid Constraint enforcementaction",
			Template:      good_rego_template,
			Constraint:    bad_enforcementaction,
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			opa, err := makeOpaClient()
			if err != nil {
				t.Fatalf("Could not initialize OPA: %s", err)
			}
			cstr := &templv1beta1.ConstraintTemplate{}
			if err := yaml.Unmarshal([]byte(tt.Template), cstr); err != nil {
				t.Fatalf("Could not instantiate template: %s", err)
			}
			unversioned := &templates.ConstraintTemplate{}
			if err := runtimeScheme.Convert(cstr, unversioned, nil); err != nil {
				t.Fatalf("Could not convert to unversioned: %v", err)
			}
			if _, err := opa.AddTemplate(context.Background(), unversioned); err != nil {
				t.Fatalf("Could not add template: %s", err)
			}
			handler := validationHandler{opa: opa}
			b, err := yaml.YAMLToJSON([]byte(tt.Constraint))
			if err != nil {
				t.Fatalf("Error parsing yaml: %s", err)
			}
			review := atypes.Request{
				AdmissionRequest: &admissionv1beta1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "constraints.gatekeeper.sh",
						Version: "v1beta1",
						Kind:    "K8sGoodRego",
					},
					Object: runtime.RawExtension{
						Raw: b,
					},
				},
			}
			_, err = handler.validateGatekeeperResources(context.Background(), review)
			if err != nil && !tt.ErrorExpected {
				t.Errorf("err = %s; want nil", err)
			}
			if err == nil && tt.ErrorExpected {
				t.Error("err = nil; want non-nil")
			}
		})
	}
}

func TestTracing(t *testing.T) {
	tc := []struct {
		Name          string
		Template      string
		User          string
		TraceExpected bool
		Cfg           *v1alpha1.Config
	}{
		{
			Name:          "Valid Trace",
			Template:      good_rego_template,
			TraceExpected: true,
			User:          "test@test.com",
			Cfg: &v1alpha1.Config{
				Spec: v1alpha1.ConfigSpec{
					Validation: v1alpha1.Validation{
						Traces: []v1alpha1.Trace{
							{
								User: "test@test.com",
								Kind: v1alpha1.GVK{
									Group:   "",
									Version: "v1",
									Kind:    "Namespace",
								},
							},
						},
					},
				},
			},
		},
		{
			Name:          "Wrong Kind",
			Template:      good_rego_template,
			TraceExpected: false,
			User:          "test@test.com",
			Cfg: &v1alpha1.Config{
				Spec: v1alpha1.ConfigSpec{
					Validation: v1alpha1.Validation{
						Traces: []v1alpha1.Trace{
							{
								User: "test@test.com",
								Kind: v1alpha1.GVK{
									Group:   "",
									Version: "v1",
									Kind:    "Pod",
								},
							},
						},
					},
				},
			},
		},
		{
			Name:          "Wrong User",
			Template:      good_rego_template,
			TraceExpected: false,
			User:          "other@test.com",
			Cfg: &v1alpha1.Config{
				Spec: v1alpha1.ConfigSpec{
					Validation: v1alpha1.Validation{
						Traces: []v1alpha1.Trace{
							{
								User: "test@test.com",
								Kind: v1alpha1.GVK{
									Group:   "",
									Version: "v1",
									Kind:    "Namespace",
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			opa, err := makeOpaClient()
			if err != nil {
				t.Fatalf("Could not initialize OPA: %s", err)
			}
			cstr := &templv1beta1.ConstraintTemplate{}
			if err := yaml.Unmarshal([]byte(tt.Template), cstr); err != nil {
				t.Fatalf("Could not instantiate template: %s", err)
			}
			unversioned := &templates.ConstraintTemplate{}
			if err := runtimeScheme.Convert(cstr, unversioned, nil); err != nil {
				t.Fatalf("Could not convert to unversioned: %v", err)
			}
			if _, err := opa.AddTemplate(context.Background(), unversioned); err != nil {
				t.Fatalf("Could not add template: %s", err)
			}
			handler := validationHandler{opa: opa, injectedConfig: tt.Cfg}
			if err != nil {
				t.Fatalf("Error parsing yaml: %s", err)
			}
			review := atypes.Request{
				AdmissionRequest: &admissionv1beta1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
					},
					Object: runtime.RawExtension{
						Raw: []byte(`{"apiVersion": "v1", "kind": "Namespace"}`),
					},
					UserInfo: authenticationv1.UserInfo{
						Username: tt.User,
					},
				},
			}
			resp, err := handler.reviewRequest(context.Background(), review)
			if err != nil {
				t.Errorf("Unexpected error: %s", err)
			}
			_, err = handler.validateGatekeeperResources(context.Background(), review)
			for _, r := range resp.ByTarget {
				if r.Trace == nil && tt.TraceExpected {
					t.Error("No trace when a trace is expected")
				}
				if r.Trace != nil && !tt.TraceExpected {
					t.Error("Trace when no trace is expected")
				}
			}
		})
	}
}
