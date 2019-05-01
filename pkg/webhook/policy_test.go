package webhook

import (
	"context"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1alpha1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	atypes "sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
)

const (
	bad_rego_template = `
apiVersion: templates.gatekeeper.sh/v1alpha1
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

        some_rule[{"msg": msg}] {
        msg := "I'm sure this will work"
`

	good_rego_template = `
apiVersion: templates.gatekeeper.sh/v1alpha1
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

        some_rule[{"msg": msg}] {
          msg := "Maybe this will work?"
        }
`

	bad_labelselector = `
apiVersion: constraints.gatekeeper.sh/v1alpha1
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
apiVersion: constraints.gatekeeper.sh/v1alpha1
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
)

func makeOpaClient() (client.Client, error) {
	target := &target.K8sValidationTarget{}
	driver := local.New(local.Tracing(true))
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
						Version: "v1alpha1",
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
			Name:          "Valid Constraint",
			Template:      good_rego_template,
			Constraint:    good_labelselector,
			ErrorExpected: false,
		},
		{
			Name:          "Invalid Constraint",
			Template:      good_rego_template,
			Constraint:    bad_labelselector,
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			opa, err := makeOpaClient()
			if err != nil {
				t.Fatalf("Could not initialize OPA: %s", err)
			}
			cstr := &v1alpha1.ConstraintTemplate{}
			if err := yaml.Unmarshal([]byte(tt.Template), cstr); err != nil {
				t.Fatalf("Could not instantiate template: %s", err)
			}
			if _, err := opa.AddTemplate(context.Background(), cstr); err != nil {
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
						Version: "v1alpha1",
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
