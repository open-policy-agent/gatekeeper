package webhook

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	templatesv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	rtypes "github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	testclients "github.com/open-policy-agent/gatekeeper/test/clients"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8schema "k8s.io/apimachinery/pkg/runtime/schema"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	atypes "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	badLabelSelector = `
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

	goodLabelSelector = `
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

	badNamespaceSelector = `
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

	goodNamespaceSelector = `
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

	goodEnforcementAction = `
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

	badEnforcementAction = `
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

	validProvider = `
apiVersion: externaldata.gatekeeper.sh/v1alpha1
kind: Provider
metadata:
  name: dummy-provider
spec:
  url: https://localhost:8080/validate
  timeout: 1
  caBundle: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUIwekNDQVgyZ0F3SUJBZ0lKQUkvTTdCWWp3Qit1TUEwR0NTcUdTSWIzRFFFQkJRVUFNRVV4Q3pBSkJnTlYKQkFZVEFrRlZNUk13RVFZRFZRUUlEQXBUYjIxbExWTjBZWFJsTVNFd0h3WURWUVFLREJoSmJuUmxjbTVsZENCWAphV1JuYVhSeklGQjBlU0JNZEdRd0hoY05NVEl3T1RFeU1qRTFNakF5V2hjTk1UVXdPVEV5TWpFMU1qQXlXakJGCk1Rc3dDUVlEVlFRR0V3SkJWVEVUTUJFR0ExVUVDQXdLVTI5dFpTMVRkR0YwWlRFaE1COEdBMVVFQ2d3WVNXNTAKWlhKdVpYUWdWMmxrWjJsMGN5QlFkSGtnVEhSa01Gd3dEUVlKS29aSWh2Y05BUUVCQlFBRFN3QXdTQUpCQU5MSgpoUEhoSVRxUWJQa2xHM2liQ1Z4d0dNUmZwL3Y0WHFoZmRRSGRjVmZIYXA2TlE1V29rLzR4SUErdWkzNS9NbU5hCnJ0TnVDK0JkWjF0TXVWQ1BGWmNDQXdFQUFhTlFNRTR3SFFZRFZSME9CQllFRkp2S3M4UmZKYVhUSDA4VytTR3YKelF5S24wSDhNQjhHQTFVZEl3UVlNQmFBRkp2S3M4UmZKYVhUSDA4VytTR3Z6UXlLbjBIOE1Bd0dBMVVkRXdRRgpNQU1CQWY4d0RRWUpLb1pJaHZjTkFRRUZCUUFEUVFCSmxmZkpIeWJqREd4Uk1xYVJtRGhYMCs2djAyVFVLWnNXCnI1UXVWYnBRaEg2dSswVWdjVzBqcDlRd3B4b1BUTFRXR1hFV0JCQnVyeEZ3aUNCaGtRK1YKLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo=
`

	providerWithNoCA = `
apiVersion: externaldata.gatekeeper.sh/v1alpha1
kind: Provider
metadata:
  name: dummy-provider
spec:
  url: https://localhost:8080/validate
  timeout: 1
`
)

func validRegoTemplate() *templates.ConstraintTemplate {
	return &templates.ConstraintTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: templatesv1beta1.SchemeGroupVersion.String(),
			Kind:       "ConstraintTemplate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "k8sgoodrego",
		},
		Spec: templates.ConstraintTemplateSpec{
			CRD: templates.CRD{
				Spec: templates.CRDSpec{
					Names: templates.Names{
						Kind: "K8sGoodRego",
					},
				},
			},
			Targets: []templates.Target{{
				Target: target.Name,
				Rego: `
package goodrego

        violation[{"msg": msg}] {
          msg := "Maybe this will work?"
        }`,
			}},
		},
	}
}

func validRegoTemplateConstraint() *unstructured.Unstructured {
	u := &unstructured.Unstructured{}

	u.SetGroupVersionKind(k8schema.GroupVersionKind{
		Group:   constraints.Group,
		Version: "v1beta1",
		Kind:    "K8sGoodRego",
	})
	u.SetName("constraint")

	return u
}

func invalidRegoTemplate() *templates.ConstraintTemplate {
	template := validRegoTemplate()

	template.Spec.Targets[0].Rego = `package badrego

        violation[{"msg": msg}] {
        msg := "I'm sure this will work"`

	return template
}

func makeOpaClient() (*constraintclient.Client, error) {
	t := &target.K8sValidationTarget{}
	driver, err := local.New(local.Tracing(false))
	if err != nil {
		return nil, err
	}

	c, err := constraintclient.NewClient(constraintclient.Targets(t), constraintclient.Driver(driver))
	if err != nil {
		return nil, err
	}
	return c, nil
}

func TestTemplateValidation(t *testing.T) {
	tc := []struct {
		Name          string
		Template      *templates.ConstraintTemplate
		ErrorExpected bool
	}{
		{
			Name:          "Valid Template",
			Template:      validRegoTemplate(),
			ErrorExpected: false,
		},
		{
			Name:          "Invalid Template",
			Template:      invalidRegoTemplate(),
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			opa, err := makeOpaClient()
			if err != nil {
				t.Fatalf("Could not initialize OPA: %s", err)
			}
			handler := validationHandler{opa: opa, webhookHandler: webhookHandler{}}

			b, err := json.Marshal(tt.Template)
			if err != nil {
				t.Fatalf("Error parsing yaml: %s", err)
			}

			review := &atypes.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
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

type nsGetter struct {
	testclients.NoopClient
}

func (f *nsGetter) Get(_ context.Context, key ctrlclient.ObjectKey, obj ctrlclient.Object) error {
	if ns, ok := obj.(*corev1.Namespace); ok {
		ns.ObjectMeta = metav1.ObjectMeta{
			Name: key.Name,
		}
		return nil
	}

	return k8serrors.NewNotFound(k8schema.GroupResource{Resource: "namespaces"}, key.Name)
}

type errorNSGetter struct {
	testclients.NoopClient
}

func (f *errorNSGetter) Get(_ context.Context, key ctrlclient.ObjectKey, _ ctrlclient.Object) error {
	return k8serrors.NewNotFound(k8schema.GroupResource{Resource: "namespaces"}, key.Name)
}

func TestReviewRequest(t *testing.T) {
	cfg := &v1alpha1.Config{
		Spec: v1alpha1.ConfigSpec{
			Validation: v1alpha1.Validation{
				Traces: []v1alpha1.Trace{},
			},
		},
	}
	tc := []struct {
		Name         string
		Template     string
		Cfg          *v1alpha1.Config
		CachedClient ctrlclient.Client
		APIReader    ctrlclient.Reader
		Error        bool
	}{
		{
			Name:         "cached client success",
			Cfg:          cfg,
			CachedClient: &nsGetter{},
			Error:        false,
		},
		{
			Name:         "cached client fail reader success",
			Cfg:          cfg,
			CachedClient: &errorNSGetter{},
			APIReader:    &nsGetter{},
			Error:        false,
		},
		{
			Name:         "reader fail",
			Cfg:          cfg,
			CachedClient: &errorNSGetter{},
			APIReader:    &errorNSGetter{},
			Error:        true,
		},
	}
	for _, tt := range tc {
		maxThreads := -1
		testFn := func(t *testing.T) {
			opa, err := makeOpaClient()
			if err != nil {
				t.Fatalf("Could not initialize OPA: %s", err)
			}
			handler := validationHandler{opa: opa, webhookHandler: webhookHandler{injectedConfig: tt.Cfg, client: tt.CachedClient, reader: tt.APIReader}}
			if maxThreads > 0 {
				handler.semaphore = make(chan struct{}, maxThreads)
			}
			review := &atypes.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
					Object: runtime.RawExtension{
						Raw: []byte(
							`{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "acbd","namespace": "ns1"}}`),
					},
					Namespace: "ns1",
				},
			}
			_, err = handler.reviewRequest(context.Background(), review)
			if err != nil && !tt.Error {
				t.Errorf("err = %s; want nil", err)
			}
			if err == nil && tt.Error {
				t.Error("err = nil; want non-nil")
			}
		}
		t.Run(tt.Name, testFn)

		maxThreads = 1
		t.Run(tt.Name+" with max threads", testFn)
	}
}

func TestReviewDefaultNS(t *testing.T) {
	cfg := &v1alpha1.Config{
		Spec: v1alpha1.ConfigSpec{
			Match: []v1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []util.Wildcard{"default"},
					Processes:          []string{"*"},
				},
			},
			Validation: v1alpha1.Validation{
				Traces: []v1alpha1.Trace{},
			},
		},
	}
	maxThreads := -1
	testFn := func(t *testing.T) {
		ctx := context.Background()
		opa, err := makeOpaClient()
		if err != nil {
			t.Fatalf("Could not initialize OPA: %s", err)
		}
		if _, err := opa.AddTemplate(ctx, validRegoTemplate()); err != nil {
			t.Fatalf("could not add template: %s", err)
		}
		if _, err := opa.AddConstraint(ctx, validRegoTemplateConstraint()); err != nil {
			t.Fatalf("could not add constraint: %s", err)
		}
		pe := process.New()
		pe.Add(cfg.Spec.Match)
		handler := validationHandler{
			opa: opa,
			webhookHandler: webhookHandler{
				injectedConfig:  cfg,
				client:          &nsGetter{},
				reader:          &nsGetter{},
				processExcluder: pe,
			},
		}
		if maxThreads > 0 {
			handler.semaphore = make(chan struct{}, maxThreads)
		}
		review := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "Pod",
				},
				Object: runtime.RawExtension{
					Raw: []byte(
						`{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "acbd","namespace": ""}}`),
				},
				Namespace: "default",
			},
		}
		resp := handler.Handle(context.Background(), review)
		if err != nil {
			t.Errorf("err = %s; want nil", err)
		}
		if !resp.Allowed {
			t.Error("allowed = false; want true")
		}
	}
	t.Run("unlimited threads", testFn)

	maxThreads = 1
	t.Run("with max threads", testFn)
}

func TestConstraintValidation(t *testing.T) {
	tc := []struct {
		Name          string
		Template      *templates.ConstraintTemplate
		Constraint    string
		ErrorExpected bool
	}{
		{
			Name:          "Valid Constraint labelselector",
			Template:      validRegoTemplate(),
			Constraint:    goodLabelSelector,
			ErrorExpected: false,
		},
		{
			Name:          "Invalid Constraint labelselector",
			Template:      validRegoTemplate(),
			Constraint:    badLabelSelector,
			ErrorExpected: true,
		},
		{
			Name:          "Valid Constraint namespaceselector",
			Template:      validRegoTemplate(),
			Constraint:    goodNamespaceSelector,
			ErrorExpected: false,
		},
		{
			Name:          "Invalid Constraint namespaceselector",
			Template:      validRegoTemplate(),
			Constraint:    badNamespaceSelector,
			ErrorExpected: true,
		},
		{
			Name:          "Valid Constraint enforcementaction",
			Template:      validRegoTemplate(),
			Constraint:    goodEnforcementAction,
			ErrorExpected: false,
		},
		{
			Name:          "Invalid Constraint enforcementaction",
			Template:      validRegoTemplate(),
			Constraint:    badEnforcementAction,
			ErrorExpected: true,
		},
	}
	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			opa, err := makeOpaClient()
			if err != nil {
				t.Fatalf("Could not initialize OPA: %s", err)
			}

			ctx := context.Background()
			if _, err := opa.AddTemplate(ctx, tt.Template); err != nil {
				t.Fatalf("Could not add template: %s", err)
			}
			handler := validationHandler{opa: opa, webhookHandler: webhookHandler{}}
			b, err := yaml.YAMLToJSON([]byte(tt.Constraint))
			if err != nil {
				t.Fatalf("Error parsing yaml: %s", err)
			}
			review := &atypes.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
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

func TestTracing(t *testing.T) {
	tc := []struct {
		Name          string
		Template      *templates.ConstraintTemplate
		User          string
		TraceExpected bool
		Cfg           *v1alpha1.Config
	}{
		{
			Name:          "Valid Trace",
			Template:      validRegoTemplate(),
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
			Template:      validRegoTemplate(),
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
			Template:      validRegoTemplate(),
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
		maxThreads := -1
		testFn := func(t *testing.T) {
			opa, err := makeOpaClient()
			if err != nil {
				t.Fatalf("Could not initialize OPA: %s", err)
			}

			ctx := context.Background()
			_, err = opa.AddTemplate(ctx, tt.Template)
			if err != nil {
				t.Fatalf("Could not add template: %s", err)
			}

			_, err = opa.AddConstraint(ctx, validRegoTemplateConstraint())
			if err != nil {
				t.Fatal(err)
			}

			handler := validationHandler{opa: opa, webhookHandler: webhookHandler{injectedConfig: tt.Cfg}}
			if maxThreads > 0 {
				handler.semaphore = make(chan struct{}, maxThreads)
			}

			review := &atypes.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
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
			_, err = handler.validateGatekeeperResources(ctx, review)
			if err != nil {
				t.Errorf("unable to validate gatekeeper resources: %s", err)
			}
			for _, r := range resp.ByTarget {
				if r.Trace == nil && tt.TraceExpected {
					t.Error("No trace when a trace is expected")
				}
				if r.Trace != nil && !tt.TraceExpected {
					t.Error("Trace when no trace is expected")
				}
			}
		}
		t.Run(tt.Name, testFn)
		maxThreads = 1
		t.Run(tt.Name+" with max threads", testFn)
	}
}

func newConstraint(kind, name string, enforcementAction string, t *testing.T) *unstructured.Unstructured {
	c := &unstructured.Unstructured{}
	c.SetGroupVersionKind(k8schema.GroupVersionKind{
		Group:   "constraints.gatekeeper.sh",
		Version: "v1alpha1",
		Kind:    kind,
	})
	c.SetName(name)
	if err := unstructured.SetNestedField(c.Object, enforcementAction, "spec", "enforcementAction"); err != nil {
		t.Errorf("unable to set enforcementAction for constraint resources: %s", err)
	}
	return c
}

func TestGetValidationMessages(t *testing.T) {
	resDryRun := &rtypes.Result{
		Msg:               "test",
		Constraint:        newConstraint("Foo", "ph", "dryrun", t),
		EnforcementAction: "dryrun",
	}
	resDeny := &rtypes.Result{
		Msg:               "test",
		Constraint:        newConstraint("Foo", "ph", "deny", t),
		EnforcementAction: "deny",
	}
	resWarn := &rtypes.Result{
		Msg:               "test",
		Constraint:        newConstraint("Foo", "ph", "warn", t),
		EnforcementAction: "warn",
	}
	resRandom := &rtypes.Result{
		Msg:               "test",
		Constraint:        newConstraint("Foo", "ph", "random", t),
		EnforcementAction: "random",
	}

	tc := []struct {
		Name                 string
		Result               []*rtypes.Result
		ExpectedDenyMsgCount int
		ExpectedWarnMsgCount int
	}{
		{
			Name: "Only One Dry Run",
			Result: []*rtypes.Result{
				resDryRun,
			},
			ExpectedDenyMsgCount: 0,
			ExpectedWarnMsgCount: 0,
		},
		{
			Name: "Only One Deny",
			Result: []*rtypes.Result{
				resDeny,
			},
			ExpectedDenyMsgCount: 1,
			ExpectedWarnMsgCount: 0,
		},
		{
			Name: "Only One Warn",
			Result: []*rtypes.Result{
				resWarn,
			},
			ExpectedDenyMsgCount: 0,
			ExpectedWarnMsgCount: 1,
		},
		{
			Name: "One Dry Run and One Deny",
			Result: []*rtypes.Result{
				resDryRun,
				resDeny,
			},
			ExpectedDenyMsgCount: 1,
			ExpectedWarnMsgCount: 0,
		},
		{
			Name: "One Dry Run, One Deny, One Warn",
			Result: []*rtypes.Result{
				resDryRun,
				resDeny,
				resWarn,
			},
			ExpectedDenyMsgCount: 1,
			ExpectedWarnMsgCount: 1,
		},
		{
			Name: "Two Deny",
			Result: []*rtypes.Result{
				resDeny,
				resDeny,
			},
			ExpectedDenyMsgCount: 2,
			ExpectedWarnMsgCount: 0,
		},
		{
			Name: "Two Warn",
			Result: []*rtypes.Result{
				resWarn,
				resWarn,
			},
			ExpectedDenyMsgCount: 0,
			ExpectedWarnMsgCount: 2,
		},
		{
			Name: "Two Dry Run",
			Result: []*rtypes.Result{
				resDryRun,
				resDryRun,
			},
			ExpectedDenyMsgCount: 0,
			ExpectedWarnMsgCount: 0,
		},
		{
			Name: "Random EnforcementAction",
			Result: []*rtypes.Result{
				resRandom,
			},
			ExpectedDenyMsgCount: 0,
			ExpectedWarnMsgCount: 0,
		},
	}

	for _, tt := range tc {
		maxThreads := -1
		testFn := func(t *testing.T) {
			opa, err := makeOpaClient()
			if err != nil {
				t.Fatalf("Could not initialize OPA: %s", err)
			}
			handler := validationHandler{opa: opa, webhookHandler: webhookHandler{}}
			if maxThreads > 0 {
				handler.semaphore = make(chan struct{}, maxThreads)
			}
			review := &atypes.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Namespace",
					},
					Object: runtime.RawExtension{
						Raw: []byte(`{"apiVersion": "v1", "kind": "Namespace"}`),
					},
				},
			}
			denyMsgs, warnMsgs := handler.getValidationMessages(tt.Result, review)
			if len(denyMsgs) != tt.ExpectedDenyMsgCount {
				t.Errorf("denyMsgs: expected count = %d; actual count = %d", tt.ExpectedDenyMsgCount, len(denyMsgs))
			}
			if len(warnMsgs) != tt.ExpectedWarnMsgCount {
				t.Errorf("warnMsgs: expected count = %d; actual count = %d", tt.ExpectedWarnMsgCount, len(warnMsgs))
			}
		}
		t.Run(tt.Name, testFn)

		maxThreads = 1
		t.Run(tt.Name+" with max threads", testFn)
	}
}

func TestValidateConfigResource(t *testing.T) {
	tc := []struct {
		TestName string
		Name     string
		Err      bool
	}{
		{
			TestName: "Wrong name",
			Name:     "FooBar",
			Err:      true,
		},
		{
			TestName: "Correct name",
			Name:     "config",
		},
	}

	for _, tt := range tc {
		t.Run(tt.TestName, func(t *testing.T) {
			handler := validationHandler{}
			req := &atypes.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: tt.Name,
				},
			}

			err := handler.validateConfigResource(req)

			if tt.Err && err == nil {
				t.Errorf("Expected error but received nil")
			}
			if !tt.Err && err != nil {
				t.Errorf("Did not expect error but received: %v", err)
			}
		})
	}
}

func TestValidateProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		want     bool
		wantErr  bool
	}{
		{
			name:     "valid provider",
			provider: validProvider,
			want:     false,
			wantErr:  false,
		},
		{
			name:     "invalid provider",
			provider: "invalid",
			want:     false,
			wantErr:  true,
		},
		{
			name:     "provider with no CA",
			provider: providerWithNoCA,
			want:     true,
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &validationHandler{}
			b, err := yaml.YAMLToJSON([]byte(tt.provider))
			if err != nil {
				t.Fatalf("Error parsing yaml: %s", err)
			}

			req := &atypes.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: runtime.RawExtension{
						Raw: b,
					},
				},
			}
			got, err := h.validateProvider(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("validationHandler.validateProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("validationHandler.validateProvider() = %v, want %v", got, tt.want)
			}
		})
	}
}
