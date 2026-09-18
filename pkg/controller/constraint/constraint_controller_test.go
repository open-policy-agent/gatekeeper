package constraint

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/go-logr/logr/funcr"
	apiconstraints "github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	templatesv1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1"
	templatesv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	regodriver "github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	regoSchema "github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego/schema"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	constraintstatusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/webhookconfig/webhookconfigcache"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel"
	celSchema "github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/transform"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/keys"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/webhook"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func makeTemplateWithRegoAndCELEngine(vapGenerationVal *bool) *templates.ConstraintTemplate {
	source := &celSchema.Source{
		Validations: []celSchema.Validation{
			{
				Expression: "1 == 1",
				Message:    "Always true",
			},
		},
		GenerateVAP: vapGenerationVal,
	}

	regoSource := &regoSchema.Source{
		Rego: `
			package foo
			
			violation[{"msg": "denied!"}] {
				1 == 1
			}
			`,
	}

	return &templates.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testkind",
		},
		Spec: templates.ConstraintTemplateSpec{
			Targets: []templates.Target{
				{
					Target: "admission.k8s.io",
					Code: []templates.Code{
						{
							Engine: celSchema.Name,
							Source: &templates.Anything{
								Value: source.MustToUnstructured(),
							},
						},
						{
							Engine: regoSchema.Name,
							Source: &templates.Anything{
								Value: regoSource.ToUnstructured(),
							},
						},
					},
				},
			},
		},
	}
}

func makeTemplateWithCELEngine(vapGenerationVal *bool) *templates.ConstraintTemplate {
	source := &celSchema.Source{
		Validations: []celSchema.Validation{
			{
				Expression: "1 == 1",
				Message:    "Always true",
			},
		},
		GenerateVAP: vapGenerationVal,
	}
	return &templates.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testkind",
		},
		Spec: templates.ConstraintTemplateSpec{
			Targets: []templates.Target{
				{
					Target: "admission.k8s.io",
					Code: []templates.Code{
						{
							Engine: celSchema.Name,
							Source: &templates.Anything{
								Value: source.MustToUnstructured(),
							},
						},
					},
				},
			},
		},
	}
}

func makeTemplateWithRegoEngine() *templates.ConstraintTemplate {
	regoSource := &regoSchema.Source{
		Rego: `
			package foo
			
			violation[{"msg": "denied!"}] {
				1 == 1
			}
			`,
	}

	return &templates.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testkind",
		},
		Spec: templates.ConstraintTemplateSpec{
			Targets: []templates.Target{
				{
					Target: "admission.k8s.io",
					Code: []templates.Code{
						{
							Engine: regoSchema.Name,
							Source: &templates.Anything{
								Value: regoSource.ToUnstructured(),
							},
						},
					},
				},
			},
		},
	}
}

func TestTotalConstraintsCache(t *testing.T) {
	constraintsCache := NewConstraintsCache()
	if len(constraintsCache.cache) != 0 {
		t.Errorf("cache: %v, wanted empty cache", spew.Sdump(constraintsCache.cache))
	}

	constraintsCache.addConstraintKey("test", tags{
		enforcementAction: util.Deny,
		status:            metrics.ActiveStatus,
	})
	if len(constraintsCache.cache) != 1 {
		t.Errorf("cache: %v, wanted cache with 1 element", spew.Sdump(constraintsCache.cache))
	}

	constraintsCache.deleteConstraintKey("test")
	if len(constraintsCache.cache) != 0 {
		t.Errorf("cache: %v, wanted empty cache", spew.Sdump(constraintsCache.cache))
	}
}

func TestShouldGenerateVAPB(t *testing.T) {
	testCases := []struct {
		name                          string
		enforcementAction             util.EnforcementAction
		defGenerateVAPB               bool
		instance                      *unstructured.Unstructured
		expectedGenerate              bool
		expectedError                 error
		expectedVAPEnforcementActions []string
	}{
		{
			name:              "defaultGenerateVAPB is false, enforcementAction is Deny",
			enforcementAction: util.Deny,
			defGenerateVAPB:   false,
			instance:          &unstructured.Unstructured{},
			expectedGenerate:  false,
		},
		{
			name:                          "defaultGenerateVAPB is true, enforcementAction is Dryrun",
			enforcementAction:             util.Dryrun,
			defGenerateVAPB:               true,
			instance:                      &unstructured.Unstructured{},
			expectedGenerate:              true,
			expectedVAPEnforcementActions: []string{"dryrun"},
		},
		{
			name:              "defaultGenerateVAPB is false, enforcementAction is Scoped, VAP ep is not set",
			enforcementAction: util.Scoped,
			defGenerateVAPB:   false,
			instance: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"enforcementAction": "scoped",
						"scopedEnforcementActions": []apiconstraints.ScopedEnforcementAction{
							{
								Action: "deny",
								EnforcementPoints: []apiconstraints.EnforcementPoint{
									{
										Name: util.WebhookEnforcementPoint,
									},
								},
							},
							{
								Action: "warn",
								EnforcementPoints: []apiconstraints.EnforcementPoint{
									{
										Name: util.WebhookEnforcementPoint,
									},
								},
							},
						},
					},
				},
			},
			expectedGenerate:              false,
			expectedVAPEnforcementActions: []string{},
		},
		{
			name:              "defaultGenerateVAPB is true, enforcementAction is Scoped, VAP ep is not set",
			enforcementAction: util.Scoped,
			defGenerateVAPB:   true,
			instance: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"enforcementAction": "scoped",
						"scopedEnforcementActions": []apiconstraints.ScopedEnforcementAction{
							{
								Action: "deny",
								EnforcementPoints: []apiconstraints.EnforcementPoint{
									{
										Name: util.AuditEnforcementPoint,
									},
								},
							},
							{
								Action: "warn",
								EnforcementPoints: []apiconstraints.EnforcementPoint{
									{
										Name: util.AuditEnforcementPoint,
									},
								},
							},
						},
					},
				},
			},
			expectedGenerate:              false,
			expectedVAPEnforcementActions: []string{},
		},
		{
			name:              "defaultGenerateVAPB is false, enforcementAction is Scoped, VAP ep is set",
			enforcementAction: util.Scoped,
			defGenerateVAPB:   false,
			instance: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"enforcementAction": "scoped",
						"scopedEnforcementActions": []apiconstraints.ScopedEnforcementAction{
							{
								Action: "deny",
								EnforcementPoints: []apiconstraints.EnforcementPoint{
									{
										Name: util.WebhookEnforcementPoint,
									},
									{
										Name: util.VAPEnforcementPoint,
									},
								},
							},
							{
								Action: "warn",
								EnforcementPoints: []apiconstraints.EnforcementPoint{
									{
										Name: util.WebhookEnforcementPoint,
									},
								},
							},
						},
					},
				},
			},
			expectedGenerate:              true,
			expectedVAPEnforcementActions: []string{"deny"},
		},
		{
			name:              "defaultGenerateVAPB is true, enforcementAction is Scoped, VAP ep is set",
			enforcementAction: util.Scoped,
			defGenerateVAPB:   true,
			instance: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"enforcementAction": "scoped",
						"scopedEnforcementActions": []apiconstraints.ScopedEnforcementAction{
							{
								Action: "deny",
								EnforcementPoints: []apiconstraints.EnforcementPoint{
									{
										Name: util.AuditEnforcementPoint,
									},
									{
										Name: util.VAPEnforcementPoint,
									},
								},
							},
							{
								Action: "warn",
								EnforcementPoints: []apiconstraints.EnforcementPoint{
									{
										Name: util.AuditEnforcementPoint,
									},
									{
										Name: util.VAPEnforcementPoint,
									},
								},
							},
						},
					},
				},
			},
			expectedGenerate:              true,
			expectedVAPEnforcementActions: []string{"deny", "warn"},
		},
		{
			name:              "defaultGenerateVAPB is true, enforcementAction is Scoped, wildcard ep is set",
			enforcementAction: util.Scoped,
			defGenerateVAPB:   true,
			instance: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"enforcementAction": "scoped",
						"scopedEnforcementActions": []apiconstraints.ScopedEnforcementAction{
							{
								Action: "deny",
								EnforcementPoints: []apiconstraints.EnforcementPoint{
									{
										Name: "*",
									},
								},
							},
							{
								Action: "warn",
								EnforcementPoints: []apiconstraints.EnforcementPoint{
									{
										Name: util.AuditEnforcementPoint,
									},
								},
							},
						},
					},
				},
			},
			expectedGenerate:              true,
			expectedVAPEnforcementActions: []string{"deny"},
		},
		{
			name:              "defaultGenerateVAPB is false, enforcementAction is Scoped, wildcard ep is set",
			enforcementAction: util.Scoped,
			defGenerateVAPB:   false,
			instance: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"enforcementAction": "scoped",
						"scopedEnforcementActions": []apiconstraints.ScopedEnforcementAction{
							{
								Action: "deny",
								EnforcementPoints: []apiconstraints.EnforcementPoint{
									{
										Name: "*",
									},
								},
							},
							{
								Action: "warn",
								EnforcementPoints: []apiconstraints.EnforcementPoint{
									{
										Name: util.AuditEnforcementPoint,
									},
								},
							},
						},
					},
				},
			},
			expectedGenerate:              true,
			expectedVAPEnforcementActions: []string{"deny"},
		},
	}

	for _, tc := range testCases {
		if tc.name == "" {
			tc.name = string(tc.enforcementAction)
		}
		t.Run(tc.name, func(t *testing.T) {
			generate, VAPEnforcementActions, err := shouldGenerateVAPB(tc.defGenerateVAPB, tc.enforcementAction, tc.instance)
			if err != nil && (err.Error() != errors.New("scopedEnforcementActions is required").Error()) {
				t.Errorf("shouldGenerateVAPB returned an unexpected error: %v", err)
			}
			if generate != tc.expectedGenerate {
				t.Errorf("shouldGenerateVAPB returned generate = %v, expected %v", generate, tc.expectedGenerate)
			}
			if !reflect.DeepEqual(VAPEnforcementActions, tc.expectedVAPEnforcementActions) {
				t.Errorf("shouldGenerateVAPB returned VAPEnforcementActions = %v, expected %v", VAPEnforcementActions, tc.expectedVAPEnforcementActions)
			}
		})
	}
}

func TestShouldGenerateVAP(t *testing.T) {
	tests := []struct {
		name       string
		template   *templates.ConstraintTemplate
		vapDefault bool
		expected   bool
		wantErr    bool
	}{
		{
			name: "missing K8sNative driver",
			template: &templates.ConstraintTemplate{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConstraintTemplate",
					APIVersion: templatesv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: strings.ToLower("ShouldNotGenerateVAP"),
				},
				Spec: templates.ConstraintTemplateSpec{
					CRD: templates.CRD{
						Spec: templates.CRDSpec{
							Names: templates.Names{
								Kind: "ShouldNotGenerateVAP",
							},
						},
					},
					Targets: []templates.Target{
						{
							Target: target.Name,
							Rego: `
								package foo
								
								violation[{"msg": "denied!"}] {
									1 == 1
								}
								`,
						},
					},
				},
			},
			vapDefault: true,
			expected:   false,
			wantErr:    true,
		},
		{
			name:       "template with only Rego engine",
			template:   makeTemplateWithRegoEngine(),
			vapDefault: true,
			expected:   false,
			wantErr:    true,
		},
		{
			name:       "Rego and CEL template with generateVAP set to true",
			template:   makeTemplateWithRegoAndCELEngine(ptr.To[bool](true)),
			vapDefault: true,
			expected:   true,
			wantErr:    false,
		},
		{
			name:       "Rego and CEL template with generateVAP set to false",
			template:   makeTemplateWithRegoAndCELEngine(ptr.To[bool](false)),
			vapDefault: true,
			expected:   false,
			wantErr:    false,
		},
		{
			name:       "Enabled, default 'no'",
			template:   makeTemplateWithCELEngine(ptr.To[bool](true)),
			vapDefault: false,
			expected:   true,
			wantErr:    false,
		},
		{
			name:       "Enabled, default 'yes'",
			template:   makeTemplateWithCELEngine(ptr.To[bool](true)),
			vapDefault: true,
			expected:   true,
			wantErr:    false,
		},
		{
			name:       "Disabled, default 'yes'",
			template:   makeTemplateWithCELEngine(ptr.To[bool](false)),
			vapDefault: true,
			expected:   false,
			wantErr:    false,
		},
		{
			name:       "Disabled, default 'no'",
			template:   makeTemplateWithCELEngine(ptr.To[bool](false)),
			vapDefault: false,
			expected:   false,
			wantErr:    false,
		},
		{
			name:       "missing, default 'yes'",
			template:   makeTemplateWithCELEngine(nil),
			vapDefault: true,
			expected:   true,
			wantErr:    false,
		},
		{
			name:       "missing, default 'no'",
			template:   makeTemplateWithCELEngine(nil),
			vapDefault: false,
			expected:   false,
			wantErr:    false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureVAP(t, vapTestConfig{defaultGenerateVAP: ptr.To(test.vapDefault)})
			generateVAP, err := ShouldGenerateVAP(test.template)
			if generateVAP != test.expected {
				t.Errorf("wanted assumeVAP to be %v; got %v", test.expected, generateVAP)
			}
			if test.wantErr != (err != nil) {
				t.Errorf("wanted error %v; got %v", test.wantErr, err)
			}
		})
	}
}

func TestReportErrorOnConstraintStatus(t *testing.T) {
	status := &constraintstatusv1beta1.ConstraintPodStatus{
		Status: constraintstatusv1beta1.ConstraintPodStatusStatus{
			Errors: []constraintstatusv1beta1.Error{{Message: "existing error"}},
		},
	}
	r := &ReconcileConstraint{}
	testErr := errors.New("test error")

	if err := r.reportErrorOnConstraintStatus(context.Background(), status, testErr, "test message"); !errors.Is(err, testErr) {
		t.Fatalf("expected original error %v, got %v", testErr, err)
	}
	expected := []constraintstatusv1beta1.Error{
		{Message: "existing error"},
		{Message: "test message: test error"},
	}
	if !reflect.DeepEqual(status.Status.Errors, expected) {
		t.Fatalf("expected status errors %v, got %v", expected, status.Status.Errors)
	}
}

func TestPersistPodStatus(t *testing.T) {
	tests := []struct {
		name        string
		change      bool
		writeErr    error
		wantUpdates int
		wantErr     bool
	}{
		{name: "unchanged"},
		{name: "changed", change: true, wantUpdates: 1},
		{name: "update error", change: true, writeErr: errors.New("update error"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := &constraintstatusv1beta1.ConstraintPodStatus{
				Status: constraintstatusv1beta1.ConstraintPodStatusStatus{ID: "test-pod"},
			}
			oldStatus := status.Status.DeepCopy()
			if tt.change {
				status.Status.Enforced = true
			}
			writer := &trackingWriter{fakeWriter: fakeWriter{updateErr: tt.writeErr}}
			r := &ReconcileConstraint{writer: writer}

			err := r.persistPodStatus(context.Background(), status, oldStatus)
			if (err != nil) != tt.wantErr {
				t.Fatalf("persistPodStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
			if len(writer.updatedObjects) != tt.wantUpdates {
				t.Fatalf("expected %d updates, got %d", tt.wantUpdates, len(writer.updatedObjects))
			}
		})
	}
}

func TestV1beta1ToV1(t *testing.T) {
	tests := []struct {
		name          string
		v1beta1Obj    *admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding
		expectedObj   *admissionregistrationv1.ValidatingAdmissionPolicyBinding
		expectedError error
	}{
		{
			name: "valid conversion",
			v1beta1Obj: &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-binding",
				},
				Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicyBindingSpec{
					PolicyName: "test-policy",
					ParamRef: &admissionregistrationv1beta1.ParamRef{
						Name: "test-param",
					},
					ValidationActions: []admissionregistrationv1beta1.ValidationAction{
						admissionregistrationv1beta1.Deny,
						admissionregistrationv1beta1.Warn,
						admissionregistrationv1beta1.Audit,
					},
					MatchResources: &admissionregistrationv1beta1.MatchResources{
						ObjectSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"key": "value"},
						},
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"key": "value"},
						},
					},
				},
			},
			expectedObj: &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-binding",
				},
				Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
					PolicyName: "test-policy",
					ParamRef: &admissionregistrationv1.ParamRef{
						Name:                    "test-param",
						ParameterNotFoundAction: ptr.To[admissionregistrationv1.ParameterNotFoundActionType](admissionregistrationv1.AllowAction),
					},
					ValidationActions: []admissionregistrationv1.ValidationAction{
						admissionregistrationv1.Deny,
						admissionregistrationv1.Warn,
						admissionregistrationv1.Audit,
					},
					MatchResources: &admissionregistrationv1.MatchResources{
						ObjectSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"key": "value"},
						},
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"key": "value"},
						},
					},
				},
			},
			expectedError: nil,
		},
		{
			name: "unrecognized enforcement action",
			v1beta1Obj: &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-binding",
				},
				Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicyBindingSpec{
					PolicyName: "test-policy",
					ParamRef: &admissionregistrationv1beta1.ParamRef{
						Name: "test-param",
					},
					ValidationActions: []admissionregistrationv1beta1.ValidationAction{
						"unknown",
					},
				},
			},
			expectedObj:   nil,
			expectedError: fmt.Errorf("%w: unrecognized enforcement action unknown, must be `warn`, `deny` or `dryrun`", transform.ErrBadEnforcementAction),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := v1beta1ToV1(tt.v1beta1Obj)
			if err != nil && tt.expectedError == nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if err == nil && tt.expectedError != nil {
				t.Fatalf("expected error %v, got none", tt.expectedError)
			}
			if err != nil && tt.expectedError != nil && err.Error() != tt.expectedError.Error() {
				t.Fatalf("expected error %v, got %v", tt.expectedError, err)
			}
			if !reflect.DeepEqual(obj, tt.expectedObj) {
				t.Errorf("expected object %v, got %v", tt.expectedObj, obj)
			}
		})
	}
}

func TestEventPackerMapFuncFromOwnerRefs_ValidOwner(t *testing.T) {
	mf := eventPackerMapFuncFromOwnerRefs()
	obj := &unstructured.Unstructured{}
	obj.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: "constraints.gatekeeper.sh/v1beta1",
		Kind:       "MyConstraint",
		Name:       "example-constraint",
		Controller: ptrBool(true),
	}})

	got := mf(context.Background(), obj)
	if len(got) != 1 {
		t.Fatalf("expected 1 request, got %d", len(got))
	}
	expectedPrefix := "gvk:MyConstraint.v1beta1.constraints.gatekeeper.sh:"
	if got[0].Name[:len(expectedPrefix)] != expectedPrefix {
		t.Fatalf("packed name not as expected: %s", got[0].Name)
	}
	// Unpack validation via util.UnpackRequest is exercised elsewhere; ensure namespace empty
	if got[0].Namespace != "" {
		t.Fatalf("expected cluster-scoped owner to produce empty namespace, got %q", got[0].Namespace)
	}
}

func TestEventPackerMapFuncFromOwnerRefs_IgnoredOwner(t *testing.T) {
	mf := eventPackerMapFuncFromOwnerRefs()
	obj := &unstructured.Unstructured{}
	obj.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Name:       "my-deploy",
		Controller: ptrBool(true),
	}})

	got := mf(context.Background(), obj)
	if len(got) != 0 {
		t.Fatalf("expected 0 requests for non-constraint owner, got %d", len(got))
	}
}

// ptrBool returns a pointer to the provided bool.
func ptrBool(b bool) *bool { return &b }

// fakeReader is a configurable fake client.Reader for testing.
type fakeReader struct {
	objects map[types.NamespacedName]client.Object
	getErr  error
	getErrs map[types.NamespacedName]error
}

func (f *fakeReader) Get(_ context.Context, key types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
	if err, ok := f.getErrs[key]; ok {
		return err
	}
	if f.getErr != nil {
		return f.getErr
	}
	stored, ok := f.objects[key]
	if !ok {
		return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
	}
	// Copy stored object data into the output parameter.
	switch dst := obj.(type) {
	case *unstructured.Unstructured:
		src, ok := stored.(*unstructured.Unstructured)
		if !ok {
			return fmt.Errorf("type mismatch: expected *unstructured.Unstructured, got %T", stored)
		}
		*dst = *src.DeepCopy()
	case *constraintstatusv1beta1.ConstraintPodStatus:
		src, ok := stored.(*constraintstatusv1beta1.ConstraintPodStatus)
		if !ok {
			return fmt.Errorf("type mismatch: expected *constraintstatusv1beta1.ConstraintPodStatus, got %T", stored)
		}
		*dst = *src.DeepCopy()
	case *templatesv1beta1.ConstraintTemplate:
		src, ok := stored.(*templatesv1beta1.ConstraintTemplate)
		if !ok {
			return fmt.Errorf("type mismatch: expected *templatesv1beta1.ConstraintTemplate, got %T", stored)
		}
		*dst = *src
	case *configv1alpha1.Config:
		src, ok := stored.(*configv1alpha1.Config)
		if !ok {
			return fmt.Errorf("type mismatch: expected Config, got %T", stored)
		}
		*dst = *src.DeepCopy()
	case *admissionregistrationv1.ValidatingWebhookConfiguration:
		src, ok := stored.(*admissionregistrationv1.ValidatingWebhookConfiguration)
		if !ok {
			return fmt.Errorf("type mismatch: expected ValidatingWebhookConfiguration, got %T", stored)
		}
		*dst = *src.DeepCopy()
	case *admissionregistrationv1.ValidatingAdmissionPolicyBinding:
		src, ok := stored.(*admissionregistrationv1.ValidatingAdmissionPolicyBinding)
		if !ok {
			return fmt.Errorf("type mismatch: expected *admissionregistrationv1.ValidatingAdmissionPolicyBinding, got %T", stored)
		}
		*dst = *src
	case *admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding:
		src, ok := stored.(*admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding)
		if !ok {
			return fmt.Errorf("type mismatch: expected *admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding, got %T", stored)
		}
		*dst = *src.DeepCopy()
	case *admissionregistrationv1.ValidatingAdmissionPolicy:
		src, ok := stored.(*admissionregistrationv1.ValidatingAdmissionPolicy)
		if !ok {
			return fmt.Errorf("type mismatch: expected *admissionregistrationv1.ValidatingAdmissionPolicy, got %T", stored)
		}
		*dst = *src.DeepCopy()
	case *admissionregistrationv1beta1.ValidatingAdmissionPolicy:
		src, ok := stored.(*admissionregistrationv1beta1.ValidatingAdmissionPolicy)
		if !ok {
			return fmt.Errorf("type mismatch: expected *admissionregistrationv1beta1.ValidatingAdmissionPolicy, got %T", stored)
		}
		*dst = *src.DeepCopy()
	default:
		return fmt.Errorf("fakeReader does not support type %T", obj)
	}
	return nil
}

func (f *fakeReader) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	return nil
}

// trackingWriter records Delete calls for assertions.
type trackingWriter struct {
	fakeWriter
	reader                      *fakeReader
	statusUpdateErr             error
	statusUpdateErrOnlyOnErrors bool
	createAttempts              int
	updateAttempts              int
	deletedObjects              []client.Object
	createdObjects              []client.Object
	updatedObjects              []client.Object
}

func (t *trackingWriter) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	t.updateAttempts++
	if status, ok := obj.(*constraintstatusv1beta1.ConstraintPodStatus); ok && t.statusUpdateErr != nil {
		if !t.statusUpdateErrOnlyOnErrors || len(status.Status.Errors) > 0 {
			return t.statusUpdateErr
		}
	}
	if err := t.fakeWriter.Update(ctx, obj, opts...); err != nil {
		return err
	}
	t.updatedObjects = append(t.updatedObjects, obj)
	t.store(obj)
	return nil
}

func (t *trackingWriter) Delete(_ context.Context, obj client.Object, _ ...client.DeleteOption) error {
	t.deletedObjects = append(t.deletedObjects, obj)
	return nil
}

func (t *trackingWriter) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	t.createAttempts++
	if err := t.fakeWriter.Create(ctx, obj, opts...); err != nil {
		return err
	}
	t.createdObjects = append(t.createdObjects, obj)
	t.store(obj)
	return nil
}

func (t *trackingWriter) store(obj client.Object) {
	if t.reader == nil {
		return
	}
	stored, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		panic(fmt.Sprintf("object %T does not implement client.Object after DeepCopyObject", obj))
	}
	t.reader.objects[client.ObjectKeyFromObject(obj)] = stored
}

// fakeReporter implements StatsReporter for testing.
type fakeReporter struct {
	vapStatuses  map[types.NamespacedName]metrics.VAPStatus
	vapbStatuses map[types.NamespacedName]metrics.VAPStatus
}

func (f *fakeReporter) reportConstraints(_ context.Context, _ tags, _ int64) error { return nil }

func (f *fakeReporter) ReportVAPStatus(name types.NamespacedName, status metrics.VAPStatus) {
	if f.vapStatuses == nil {
		f.vapStatuses = make(map[types.NamespacedName]metrics.VAPStatus)
	}
	f.vapStatuses[name] = status
}

func (f *fakeReporter) DeleteVAPStatus(name types.NamespacedName) {
	delete(f.vapStatuses, name)
}

func (f *fakeReporter) ReportVAPBStatus(name types.NamespacedName, status metrics.VAPStatus) {
	if f.vapbStatuses == nil {
		f.vapbStatuses = make(map[types.NamespacedName]metrics.VAPStatus)
	}
	f.vapbStatuses[name] = status
}

func (f *fakeReporter) DeleteVAPBStatus(name types.NamespacedName) {
	delete(f.vapbStatuses, name)
}

func TestPersistConstraintPodStatusSkipsNoopAndWritesChanges(t *testing.T) {
	ctx := context.Background()
	status := &constraintstatusv1beta1.ConstraintPodStatus{
		Status: constraintstatusv1beta1.ConstraintPodStatusStatus{
			ConstraintUID:      "constraint-uid",
			ObservedGeneration: 1,
			Enforced:           true,
		},
	}
	writer := &trackingWriter{}
	reconciler := &ReconcileConstraint{writer: writer}

	oldStatus := status.Status.DeepCopy()
	if err := reconciler.persistPodStatus(ctx, status, oldStatus); err != nil {
		t.Fatalf("persistPodStatus() error = %v, want nil", err)
	}
	if len(writer.updatedObjects) != 0 {
		t.Fatalf("updates = %d, want 0 for unchanged status", len(writer.updatedObjects))
	}

	status.Status.ObservedGeneration++
	if err := reconciler.persistPodStatus(ctx, status, oldStatus); err != nil {
		t.Fatalf("persistPodStatus() after status change error = %v, want nil", err)
	}
	if len(writer.updatedObjects) != 1 {
		t.Fatalf("updates = %d, want 1 for changed status", len(writer.updatedObjects))
	}
}

func BenchmarkPersistConstraintPodStatusNoop(b *testing.B) {
	ctx := context.Background()
	status := &constraintstatusv1beta1.ConstraintPodStatus{
		Status: constraintstatusv1beta1.ConstraintPodStatusStatus{
			ConstraintUID:      "constraint-uid",
			ObservedGeneration: 1,
			Enforced:           true,
		},
	}
	writer := &trackingWriter{}
	reconciler := &ReconcileConstraint{writer: writer}
	oldStatus := status.Status.DeepCopy()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := reconciler.persistPodStatus(ctx, status, oldStatus); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(len(writer.updatedObjects))/float64(b.N), "updates/op")
}

func newConstraintUnitReconciler(t *testing.T, ct *templates.ConstraintTemplate, instance *unstructured.Unstructured) (*ReconcileConstraint, *fakeReader, *trackingWriter, reconcile.Request) {
	t.Helper()
	t.Setenv("POD_NAME", "test-pod")

	scheme := runtime.NewScheme()
	if err := templatesv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := constraintstatusv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := admissionregistrationv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := admissionregistrationv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	versionedCT := &templatesv1beta1.ConstraintTemplate{}
	if err := scheme.Convert(ct, versionedCT, nil); err != nil {
		t.Fatal(err)
	}

	regoDriver, err := regodriver.New()
	if err != nil {
		t.Fatal(err)
	}
	celDriver, err := k8scel.New()
	if err != nil {
		t.Fatal(err)
	}
	cfClient, err := constraintclient.NewClient(
		constraintclient.Targets(&target.K8sValidationTarget{}),
		constraintclient.Driver(regoDriver),
		constraintclient.Driver(celDriver),
		constraintclient.EnforcementPoints(util.AuditEnforcementPoint),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfClient.AddTemplate(context.Background(), ct); err != nil {
		t.Fatal(err)
	}
	if _, err := cfClient.AddConstraint(context.Background(), instance.DeepCopy()); err != nil {
		t.Fatal(err)
	}

	reader := &fakeReader{
		objects: map[types.NamespacedName]client.Object{
			client.ObjectKeyFromObject(instance): instance.DeepCopy(),
			{Name: versionedCT.GetName()}:        versionedCT,
		},
		getErrs: make(map[types.NamespacedName]error),
	}
	writer := &trackingWriter{reader: reader}
	tracker := readiness.NewTracker(reader, false, false, false)
	trackerCtx, cancelTracker := context.WithCancel(context.Background())
	t.Cleanup(cancelTracker)
	go func() {
		_ = tracker.Run(trackerCtx)
	}()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: util.GetNamespace()}}
	r := &ReconcileConstraint{
		reader:           reader,
		apiReader:        reader,
		writer:           writer,
		scheme:           scheme,
		cfClient:         cfClient,
		log:              logf.Log.WithName("test"),
		reporter:         &fakeReporter{},
		constraintsCache: NewConstraintsCache(),
		tracker:          tracker,
		getPod:           func(context.Context) (*corev1.Pod, error) { return pod, nil },
		ifWatching:       func(_ schema.GroupVersionKind, fn func() error) (bool, error) { return true, fn() },
	}
	requests := util.EventPackerMapFunc()(context.Background(), instance)
	if len(requests) != 1 {
		t.Fatalf("expected one packed request, got %d", len(requests))
	}
	return r, reader, writer, requests[0]
}

func makeUnitConstraint() *unstructured.Unstructured {
	instance := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{
			"enforcementAction": "dryrun",
		},
	}}
	instance.SetGroupVersionKind(schema.GroupVersionKind{Group: constraintstatusv1beta1.ConstraintsGroup, Version: "v1beta1", Kind: "TestKind"})
	instance.SetName("test-constraint")
	instance.SetUID("constraint-uid")
	instance.SetGeneration(1)
	return instance
}

type vapTestConfig struct {
	apiEnabled          *bool
	defaultGenerateVAP  *bool
	defaultGenerateVAPB *bool
	generationMode      *VAPGenerationMode
}

func configureVAP(t *testing.T, config vapTestConfig) {
	t.Helper()

	if config.apiEnabled != nil {
		transform.SetVapAPIEnabled(config.apiEnabled)
		if *config.apiEnabled {
			transform.SetGroupVersion(&admissionregistrationv1.SchemeGroupVersion)
		} else {
			transform.SetGroupVersion(nil)
		}
		t.Cleanup(func() {
			transform.SetVapAPIEnabled(nil)
			transform.SetGroupVersion(nil)
		})
	}

	if config.defaultGenerateVAP != nil {
		original := GetDefaultGenerateVAP()
		SetDefaultGenerateVAP(*config.defaultGenerateVAP)
		t.Cleanup(func() { SetDefaultGenerateVAP(original) })
	}

	if config.defaultGenerateVAPB != nil {
		original := GetDefaultGenerateVAPB()
		SetDefaultGenerateVAPB(*config.defaultGenerateVAPB)
		t.Cleanup(func() { SetDefaultGenerateVAPB(original) })
	}

	if config.generationMode != nil {
		original := GetVAPGenerationMode()
		if err := SetVAPGenerationMode(*config.generationMode); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := SetVAPGenerationMode(original); err != nil {
				t.Errorf("restore VAP generation mode: %v", err)
			}
		})
	}
}

func TestManageVAPB_PerConstraintModeCreatesVAPBeforeBinding(t *testing.T) {
	configureVAP(t, vapTestConfig{
		apiEnabled:          ptr.To(true),
		defaultGenerateVAP:  ptr.To(true),
		defaultGenerateVAPB: ptr.To(true),
		generationMode:      ptr.To(VAPGenerationModeConstraint),
	})
	ct := makeUnitCELTemplate()
	instance := makeUnitConstraint()
	r, _, writer, _ := newConstraintUnitReconciler(t, ct, instance)
	if err := unstructured.SetNestedField(instance.Object, map[string]interface{}{
		"message": "required",
		"labels":  []interface{}{map[string]interface{}{"key": "owner"}},
	}, "spec", "parameters"); err != nil {
		t.Fatal(err)
	}
	status := &constraintstatusv1beta1.ConstraintPodStatus{}

	delay, err := r.manageVAPB(context.Background(), util.Dryrun, instance, status)
	if err != nil {
		t.Fatal(err)
	}
	if delay != 0 {
		t.Fatalf("delay = %v, want zero", delay)
	}
	if len(writer.createdObjects) != 2 {
		t.Fatalf("created objects = %d, want 2", len(writer.createdObjects))
	}
	policy, ok := writer.createdObjects[0].(*admissionregistrationv1.ValidatingAdmissionPolicy)
	if !ok {
		t.Fatalf("first created object = %T, want VAP", writer.createdObjects[0])
	}
	binding, ok := writer.createdObjects[1].(*admissionregistrationv1.ValidatingAdmissionPolicyBinding)
	if !ok {
		t.Fatalf("second created object = %T, want VAPBinding", writer.createdObjects[1])
	}
	if policy.Spec.ParamKind != nil {
		t.Fatalf("ParamKind = %#v, want nil", policy.Spec.ParamKind)
	}
	if binding.Spec.ParamRef != nil {
		t.Fatalf("ParamRef = %#v, want nil", binding.Spec.ParamRef)
	}
	if binding.Spec.PolicyName != policy.Name || binding.Name == policy.Name {
		t.Fatalf("binding policy/name = %q/%q, policy = %q", binding.Spec.PolicyName, binding.Name, policy.Name)
	}
	if !metav1.IsControlledBy(policy, instance) || !metav1.IsControlledBy(binding, instance) {
		t.Fatal("generated VAP and VAPBinding must be controlled by the Constraint")
	}
}

func TestManageVAPB_PerConstraintModeRejectsUnownedVAP(t *testing.T) {
	for _, groupVersion := range []schema.GroupVersion{admissionregistrationv1.SchemeGroupVersion, admissionregistrationv1beta1.SchemeGroupVersion} {
		t.Run(groupVersion.Version, func(t *testing.T) {
			configureVAP(t, vapTestConfig{
				apiEnabled:          ptr.To(true),
				defaultGenerateVAP:  ptr.To(true),
				defaultGenerateVAPB: ptr.To(true),
				generationMode:      ptr.To(VAPGenerationModeConstraint),
			})
			transform.SetGroupVersion(&groupVersion)
			for _, scenario := range []struct {
				name      string
				ownerName string
				ownerUID  types.UID
			}{
				{name: "ownerless"},
				{name: "other constraint", ownerName: "other-constraint", ownerUID: "other-uid"},
				{name: "recreated constraint", ownerName: "test-constraint", ownerUID: "old-constraint-uid"},
			} {
				for _, existingBinding := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/existing-binding=%t", scenario.name, existingBinding), func(t *testing.T) {
						template := makeUnitCELTemplate()
						instance := makeUnitConstraint()
						reconciler, reader, writer, _ := newConstraintUnitReconciler(t, template, instance)
						policy, err := vapForVersion(&groupVersion)
						if err != nil {
							t.Fatal(err)
						}
						policy.SetName(transform.GetConstraintVAPName(instance.GetKind(), instance.GetName()))
						policy.SetUID("policy-uid")
						policy.SetResourceVersion("1")
						if scenario.ownerUID != "" {
							policy.SetOwnerReferences([]metav1.OwnerReference{{
								APIVersion: instance.GetAPIVersion(),
								Kind:       instance.GetKind(),
								Name:       scenario.ownerName,
								UID:        scenario.ownerUID,
								Controller: ptr.To(true),
							}})
						}
						policyKey := client.ObjectKeyFromObject(policy)
						reader.objects[policyKey] = policy
						policyBefore := policy.DeepCopyObject()
						bindingKey := types.NamespacedName{Name: transform.GetVAPBindingName(instance.GetKind(), instance.GetName())}
						var bindingBefore runtime.Object
						if existingBinding {
							transformedBinding, err := transform.ConstraintToInlinedBinding(instance, []string{string(util.Dryrun)})
							if err != nil {
								t.Fatal(err)
							}
							binding, err := getRunTimeVAPBinding(&groupVersion, transformedBinding, nil)
							if err != nil {
								t.Fatal(err)
							}
							binding.SetUID("binding-uid")
							reader.objects[bindingKey] = binding
							bindingBefore = binding.DeepCopyObject()
						}
						status := &constraintstatusv1beta1.ConstraintPodStatus{}
						reporter := &fakeReporter{}
						reconciler.reporter = reporter
						_, err = reconciler.manageVAPB(context.Background(), util.Dryrun, instance, status)
						wantError := fmt.Sprintf("validatingadmissionpolicy %q exists but is not controlled by constraint %q", policy.GetName(), instance.GetName())
						if err == nil || err.Error() != wantError {
							t.Fatalf("manageVAPB() error = %v, want %q", err, wantError)
						}
						if writer.createAttempts != 0 || writer.updateAttempts != 0 || len(writer.deletedObjects) != 0 {
							t.Fatalf("ownership conflict: creates=%d, updates=%d, deletes=%d; want 0/0/0", writer.createAttempts, writer.updateAttempts, len(writer.deletedObjects))
						}
						if !reflect.DeepEqual(policyBefore, reader.objects[policyKey]) {
							t.Fatal("ownership conflict modified the existing policy")
						}
						binding, found := reader.objects[bindingKey]
						if found != existingBinding || (found && !reflect.DeepEqual(bindingBefore, binding)) {
							t.Fatal("ownership conflict created or modified a binding")
						}
						if len(status.Status.Errors) != 1 || !strings.Contains(status.Status.Errors[0].Message, wantError) {
							t.Fatalf("constraint status errors = %v, want ownership conflict", status.Status.Errors)
						}
						if reporter.vapStatuses[policyKey] != metrics.VAPStatusError || reporter.vapbStatuses[bindingKey] != metrics.VAPStatusError {
							t.Fatalf("VAP/VAPBinding status = %v/%v, want error/error", reporter.vapStatuses[policyKey], reporter.vapbStatuses[bindingKey])
						}
					})
				}
			}
		})
	}
}

func TestManageVAPB_OperationMismatchWarning(t *testing.T) {
	configureVAP(t, vapTestConfig{
		apiEnabled: ptr.To(true), defaultGenerateVAP: ptr.To(true),
		defaultGenerateVAPB: ptr.To(true), generationMode: ptr.To(VAPGenerationModeConstraint),
	})
	originalSync := *transform.SyncVAPScope
	*transform.SyncVAPScope = true
	t.Cleanup(func() { *transform.SyncVAPScope = originalSync })
	template := makeUnitCELTemplate()
	template.Spec.Targets[0].Operations = []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update}
	instance := makeUnitConstraint()
	reconciler, reader, _, _ := newConstraintUnitReconciler(t, template, instance)
	var logEntries []string
	logger := funcr.New(func(_ string, entry string) { logEntries = append(logEntries, entry) }, funcr.Options{})
	reconciler.log = logger.V(logging.DebugLevel)
	ctx := logf.IntoContext(context.Background(), logger)
	reconciler.webhookConfigCache = webhookconfigcache.NewWebhookConfigCache()
	status := &constraintstatusv1beta1.ConstraintPodStatus{}
	for _, test := range []struct {
		name       string
		operations []admissionregistrationv1.OperationType
		warning    string
	}{
		{name: "create", operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create}, warning: transform.ErrOperationMismatch.Error()},
		{name: "unchanged", operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create}, warning: transform.ErrOperationMismatch.Error()},
		{name: "resolved", operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update}},
	} {
		t.Run(test.name, func(t *testing.T) {
			logEntries = nil
			reconciler.webhookConfigCache.UpsertConfig(*webhook.VwhName, webhookconfigcache.WebhookMatchingConfig{
				Rules: []admissionregistrationv1.RuleWithOperations{{
					Operations: test.operations,
					Rule:       admissionregistrationv1.Rule{APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}},
				}},
			})
			if delay, err := reconciler.manageVAPB(ctx, util.Dryrun, instance, status); err != nil || delay != 0 {
				t.Fatalf("generation with warning: delay=%s, err=%v", delay, err)
			}
			if test.warning != "" {
				if len(logEntries) != 1 || !strings.Contains(logEntries[0], test.warning) || !strings.Contains(logEntries[0], transform.GetConstraintVAPName(instance.GetKind(), instance.GetName())) {
					t.Fatalf("INFO logs must identify the VAP and warning: %v", logEntries)
				}
			} else if len(logEntries) != 0 {
				t.Fatalf("resolved warning still logged: %v", logEntries)
			}
			if len(status.Status.Errors) != 0 || len(status.Status.EnforcementPointsStatus) != 1 {
				t.Fatalf("unexpected status: %+v", status.Status)
			}
			point := status.Status.EnforcementPointsStatus[0]
			if point.State != GeneratedVAPBState || point.Message != test.warning || point.ObservedGeneration != instance.GetGeneration() {
				t.Fatalf("generation status = %+v, want generated with warning %q", point, test.warning)
			}
			policy, ok := reader.objects[types.NamespacedName{Name: transform.GetConstraintVAPName(instance.GetKind(), instance.GetName())}].(*admissionregistrationv1.ValidatingAdmissionPolicy)
			if !ok {
				t.Fatal("expected a generated v1 policy")
			}
			if !reflect.DeepEqual(policy.Spec.MatchConstraints.ResourceRules[0].Operations, test.operations) {
				t.Fatalf("generated operations = %v, want %v", policy.Spec.MatchConstraints.ResourceRules[0].Operations, test.operations)
			}
		})
	}
}

func TestManageVAPB_PerConstraintModeParameterUpdateOnlyUpdatesVAP(t *testing.T) {
	configureVAP(t, vapTestConfig{
		apiEnabled:          ptr.To(true),
		defaultGenerateVAP:  ptr.To(true),
		defaultGenerateVAPB: ptr.To(true),
		generationMode:      ptr.To(VAPGenerationModeConstraint),
	})
	ct := makeUnitCELTemplate()
	instance := makeUnitConstraint()
	r, _, writer, _ := newConstraintUnitReconciler(t, ct, instance)
	if err := unstructured.SetNestedField(instance.Object, map[string]interface{}{"message": "first"}, "spec", "parameters"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.manageVAPB(context.Background(), util.Dryrun, instance, &constraintstatusv1beta1.ConstraintPodStatus{}); err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedField(instance.Object, map[string]interface{}{"message": "second"}, "spec", "parameters"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.manageVAPB(context.Background(), util.Dryrun, instance, &constraintstatusv1beta1.ConstraintPodStatus{}); err != nil {
		t.Fatal(err)
	}
	if len(writer.updatedObjects) != 1 {
		t.Fatalf("updated objects = %d, want only the VAP", len(writer.updatedObjects))
	}
	policy, ok := writer.updatedObjects[0].(*admissionregistrationv1.ValidatingAdmissionPolicy)
	if !ok {
		t.Fatalf("updated object = %T, want VAP", writer.updatedObjects[0])
	}
	if !strings.Contains(policy.Spec.Variables[1].Expression, "second") {
		t.Fatalf("params expression = %q, want updated value", policy.Spec.Variables[1].Expression)
	}
}

func TestReconcileConstraintVAP_DefaultedPolicy(t *testing.T) {
	originalSync := *transform.SyncVAPScope
	*transform.SyncVAPScope = false
	t.Cleanup(func() { *transform.SyncVAPScope = originalSync })
	for _, version := range []string{vapAPIVersionV1, vapAPIVersionV1Beta1} {
		t.Run(version, func(t *testing.T) {
			template := makeUnitCELTemplate()
			instance := makeUnitConstraint()
			reconciler, reader, writer, _ := newConstraintUnitReconciler(t, template, instance)
			if err := unstructured.SetNestedField(instance.Object, map[string]interface{}{"message": "first"}, "spec", "parameters"); err != nil {
				t.Fatal(err)
			}
			groupVersion := schema.GroupVersion{Group: admissionregistrationv1.GroupName, Version: version}
			ctx := context.Background()
			if _, err := reconciler.reconcileConstraintVAP(ctx, template, instance, &groupVersion); err != nil {
				t.Fatal(err)
			}
			key := types.NamespacedName{Name: transform.GetConstraintVAPName(instance.GetKind(), instance.GetName())}
			transformed, err := reconciler.transformConstraintToVAP(template, instance)
			if err != nil {
				t.Fatal(err)
			}
			transformed.Spec.MatchConstraints.MatchPolicy = ptr.To(admissionregistrationv1beta1.Equivalent)
			transformed.Spec.MatchConstraints.NamespaceSelector = &metav1.LabelSelector{}
			transformed.Spec.MatchConstraints.ObjectSelector = &metav1.LabelSelector{}
			for index := range transformed.Spec.MatchConstraints.ResourceRules {
				transformed.Spec.MatchConstraints.ResourceRules[index].Scope = ptr.To(admissionregistrationv1.AllScopes)
			}
			defaulted, err := getRunTimeVAP(&groupVersion, transformed, reader.objects[key])
			if err != nil {
				t.Fatal(err)
			}
			defaulted.SetUID("policy-uid")
			defaulted.SetResourceVersion("1")
			reader.objects[key] = defaulted
			before := defaulted.DeepCopyObject()
			for attempt := 0; attempt < 3; attempt++ {
				if _, err := reconciler.reconcileConstraintVAP(ctx, template, instance, &groupVersion); err != nil {
					t.Fatal(err)
				}
			}
			if writer.createAttempts != 1 || writer.updateAttempts != 0 || len(writer.deletedObjects) != 0 {
				t.Fatalf("unchanged defaulted policy: creates=%d, updates=%d, deletes=%d; want 1/0/0", writer.createAttempts, writer.updateAttempts, len(writer.deletedObjects))
			}
			if !reflect.DeepEqual(before, reader.objects[key]) {
				t.Fatal("comparison mutated the existing policy")
			}

			if err := unstructured.SetNestedField(instance.Object, map[string]interface{}{"message": "second"}, "spec", "parameters"); err != nil {
				t.Fatal(err)
			}
			transformed, err = reconciler.transformConstraintToVAP(template, instance)
			if err != nil {
				t.Fatal(err)
			}
			want, err := getRunTimeVAP(&groupVersion, transformed, reader.objects[key])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reconciler.reconcileConstraintVAP(ctx, template, instance, &groupVersion); err != nil {
				t.Fatal(err)
			}
			if writer.updateAttempts != 1 || len(writer.updatedObjects) != 1 {
				t.Fatalf("changed parameters: updates=%d, want 1", writer.updateAttempts)
			}
			if !reflect.DeepEqual(want, writer.updatedObjects[0]) {
				t.Fatal("update must preserve the desired policy rather than its normalized comparison copy")
			}
			if !reflect.DeepEqual(before, defaulted) {
				t.Fatal("comparison mutated the previous policy")
			}
		})
	}
}

func TestManageVAPB_RollbackWaitsForCurrentSharedPolicy(t *testing.T) {
	for _, version := range []string{vapAPIVersionV1, vapAPIVersionV1Beta1} {
		t.Run(version, func(t *testing.T) {
			configureVAP(t, vapTestConfig{
				apiEnabled: ptr.To(true), defaultGenerateVAP: ptr.To(true),
				defaultGenerateVAPB: ptr.To(true), generationMode: ptr.To(VAPGenerationModeConstraint),
			})
			groupVersion := schema.GroupVersion{Group: admissionregistrationv1.GroupName, Version: version}
			transform.SetGroupVersion(&groupVersion)
			template := makeUnitCELTemplate()
			template.SetUID("template-uid")
			template.SetGeneration(2)
			template.SetAnnotations(map[string]string{VAPBGenerationAnnotation: VAPBGenerationUnblocked})
			instance := makeUnitConstraint()
			reconciler, reader, writer, _ := newConstraintUnitReconciler(t, template, instance)
			status := &constraintstatusv1beta1.ConstraintPodStatus{}
			if _, err := reconciler.manageVAPB(context.Background(), util.Dryrun, instance, status); err != nil {
				t.Fatal(err)
			}
			shared, err := transform.TemplateToPolicyDefinition(template)
			if err != nil {
				t.Fatal(err)
			}
			shared.SetOwnerReferences([]metav1.OwnerReference{{
				APIVersion: templatesv1beta1.SchemeGroupVersion.String(), Kind: "ConstraintTemplate",
				Name: template.GetName(), UID: template.GetUID(), Controller: ptr.To(true),
			}})
			stale := shared.DeepCopy()
			stale.Spec.Validations[0].Expression = "false"
			stalePolicy, err := getRunTimeVAP(&groupVersion, stale, nil)
			if err != nil {
				t.Fatal(err)
			}
			reader.objects[types.NamespacedName{Name: shared.GetName()}] = stalePolicy
			bindingKey := types.NamespacedName{Name: transform.GetVAPBindingName(instance.GetKind(), instance.GetName())}
			beforeBinding := reader.objects[bindingKey].DeepCopyObject()
			writer.updatedObjects, writer.deletedObjects = nil, nil
			if err := SetVAPGenerationMode(VAPGenerationModeTemplate); err != nil {
				t.Fatal(err)
			}
			delay, err := reconciler.manageVAPB(context.Background(), util.Dryrun, instance, status)
			if err != nil || delay != time.Second {
				t.Fatalf("stale policy rollback: delay=%s, err=%v; want retry", delay, err)
			}
			if len(writer.updatedObjects) != 0 || len(writer.deletedObjects) != 0 || !reflect.DeepEqual(beforeBinding, reader.objects[bindingKey]) {
				t.Fatal("stale shared policy must not change the binding or delete the specialized policy")
			}
			currentPolicy, err := getRunTimeVAP(&groupVersion, shared, nil)
			if err != nil {
				t.Fatal(err)
			}
			reader.objects[types.NamespacedName{Name: shared.GetName()}] = currentPolicy
			if delay, err := reconciler.manageVAPB(context.Background(), util.Dryrun, instance, status); err != nil || delay != 0 {
				t.Fatalf("current policy rollback: delay=%s, err=%v", delay, err)
			}
			if len(writer.updatedObjects) != 1 || len(writer.deletedObjects) != 1 {
				t.Fatalf("current policy rollback: updates=%d, deletes=%d", len(writer.updatedObjects), len(writer.deletedObjects))
			}
		})
	}
}

func TestSharedVAPIsCurrent(t *testing.T) {
	configureVAP(t, vapTestConfig{defaultGenerateVAP: ptr.To(true)})
	originalSync := *transform.SyncVAPScope
	*transform.SyncVAPScope = true
	t.Cleanup(func() { *transform.SyncVAPScope = originalSync })
	for _, version := range []string{vapAPIVersionV1, vapAPIVersionV1Beta1} {
		for _, scenario := range []string{"current", "defaulted", "old owner", "stale scope", "cache ahead", "API error", "Config cache cold", "webhook cache cold", "Config unavailable", "webhook unavailable", "generation disabled"} {
			t.Run(version+"/"+scenario, func(t *testing.T) {
				template := makeUnitCELTemplate()
				template.SetUID("current-template")
				instance := makeUnitConstraint()
				reconciler, reader, _, _ := newConstraintUnitReconciler(t, template, instance)
				groupVersion := schema.GroupVersion{Group: admissionregistrationv1.GroupName, Version: version}
				shared, err := transform.TemplateToPolicyDefinition(template)
				if err != nil {
					t.Fatal(err)
				}
				shared.SetOwnerReferences([]metav1.OwnerReference{{
					APIVersion: templatesv1beta1.SchemeGroupVersion.String(), Kind: "ConstraintTemplate",
					Name: template.GetName(), UID: template.GetUID(), Controller: ptr.To(true),
				}})
				want := scenario == "current" || scenario == "defaulted"
				switch scenario {
				case "defaulted":
					shared.Spec.MatchConstraints.MatchPolicy = ptr.To(admissionregistrationv1beta1.Equivalent)
					shared.Spec.MatchConstraints.NamespaceSelector = &metav1.LabelSelector{}
					shared.Spec.MatchConstraints.ObjectSelector = &metav1.LabelSelector{}
				case "old owner":
					shared.OwnerReferences[0].UID = "old-template"
				case "stale scope":
					shared.Spec.MatchConstraints.ObjectSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"old": "scope"}}
				case "cache ahead":
					authoritative := &fakeReader{objects: map[types.NamespacedName]client.Object{
						{Name: template.GetName()}: reader.objects[types.NamespacedName{Name: template.GetName()}],
					}}
					reconciler.apiReader = authoritative
					stale := shared.DeepCopy()
					stale.Spec.Validations[0].Expression = "false"
					stalePolicy, err := getRunTimeVAP(&groupVersion, stale, nil)
					if err != nil {
						t.Fatal(err)
					}
					authoritative.objects[types.NamespacedName{Name: shared.GetName()}] = stalePolicy
				case "API error":
					reconciler.apiReader = &fakeReader{getErr: errors.New("API unavailable")}
				case "Config cache cold":
					reader.objects[keys.Config] = &configv1alpha1.Config{
						ObjectMeta: metav1.ObjectMeta{Name: keys.Config.Name, Namespace: keys.Config.Namespace},
						Spec:       configv1alpha1.ConfigSpec{Match: []configv1alpha1.MatchEntry{{Processes: []string{"webhook"}, ExcludedNamespaces: []wildcard.Wildcard{"new-*"}}}},
					}
				case "webhook cache cold":
					reader.objects[types.NamespacedName{Name: *webhook.VwhName}] = &admissionregistrationv1.ValidatingWebhookConfiguration{
						ObjectMeta: metav1.ObjectMeta{Name: *webhook.VwhName},
						Webhooks: []admissionregistrationv1.ValidatingWebhook{{
							Name: webhook.ValidatingWebhookName,
							Rules: []admissionregistrationv1.RuleWithOperations{{
								Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
								Rule:       admissionregistrationv1.Rule{APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}},
							}},
						}},
					}
				case "Config unavailable":
					reader.getErrs[keys.Config] = errors.New("Config read failed")
				case "webhook unavailable":
					reader.getErrs[types.NamespacedName{Name: *webhook.VwhName}] = errors.New("webhook read failed")
				case "generation disabled":
					disabled := template.DeepCopy()
					source, err := celSchema.GetSourceFromTemplate(disabled)
					if err != nil {
						t.Fatal(err)
					}
					source.GenerateVAP = ptr.To(false)
					disabled.Spec.Targets[0].Code[0].Source = &templates.Anything{Value: source.MustToUnstructured()}
					versioned := &templatesv1beta1.ConstraintTemplate{}
					if err := reconciler.scheme.Convert(disabled, versioned, nil); err != nil {
						t.Fatal(err)
					}
					reader.objects[types.NamespacedName{Name: template.GetName()}] = versioned
				}
				policy, err := getRunTimeVAP(&groupVersion, shared, nil)
				if err != nil {
					t.Fatal(err)
				}
				reader.objects[types.NamespacedName{Name: shared.GetName()}] = policy
				got, err := reconciler.sharedVAPIsCurrent(context.Background(), template.GetName(), &groupVersion)
				wantErr := scenario == "API error" || strings.HasSuffix(scenario, "unavailable")
				if (err != nil) != wantErr || got != want {
					t.Fatalf("current=%v err=%v, want current=%v", got, err, want)
				}
			})
		}
	}
}

func TestManageVAPB_RollbackUpdatesBindingBeforeDeletingVAP(t *testing.T) {
	configureVAP(t, vapTestConfig{
		apiEnabled:          ptr.To(true),
		defaultGenerateVAP:  ptr.To(true),
		defaultGenerateVAPB: ptr.To(true),
		generationMode:      ptr.To(VAPGenerationModeConstraint),
	})
	ct := makeUnitCELTemplate()
	ct.SetAnnotations(map[string]string{VAPBGenerationAnnotation: VAPBGenerationUnblocked})
	instance := makeUnitConstraint()
	r, reader, writer, _ := newConstraintUnitReconciler(t, ct, instance)
	if _, err := r.manageVAPB(context.Background(), util.Dryrun, instance, &constraintstatusv1beta1.ConstraintPodStatus{}); err != nil {
		t.Fatal(err)
	}
	writer.updatedObjects = nil
	writer.deletedObjects = nil
	if err := SetVAPGenerationMode(VAPGenerationModeTemplate); err != nil {
		t.Fatal(err)
	}
	status := &constraintstatusv1beta1.ConstraintPodStatus{}
	requeueAfter, err := r.manageVAPB(context.Background(), util.Dryrun, instance, status)
	if err != nil {
		t.Fatal(err)
	}
	if requeueAfter != time.Second {
		t.Fatalf("requeueAfter = %s, want 1s while shared VAP is missing", requeueAfter)
	}
	if len(writer.updatedObjects) != 0 || len(writer.deletedObjects) != 0 {
		t.Fatalf("updated/deleted objects = %d/%d while shared VAP is missing", len(writer.updatedObjects), len(writer.deletedObjects))
	}
	reader.objects[types.NamespacedName{Name: transform.GetTemplateVAPName(instance.GetKind())}] = makeUnitTemplateVAP(t, ct, &admissionregistrationv1.SchemeGroupVersion)
	if _, err := r.manageVAPB(context.Background(), util.Dryrun, instance, status); err != nil {
		t.Fatal(err)
	}
	if len(writer.updatedObjects) != 1 {
		t.Fatalf("updated objects = %d, want binding update", len(writer.updatedObjects))
	}
	binding, ok := writer.updatedObjects[0].(*admissionregistrationv1.ValidatingAdmissionPolicyBinding)
	if !ok {
		t.Fatalf("updated object = %T, want VAPBinding", writer.updatedObjects[0])
	}
	if binding.Spec.PolicyName != "gatekeeper-testkind" || binding.Spec.ParamRef == nil {
		t.Fatalf("rolled-back binding spec = %#v", binding.Spec)
	}
	if len(writer.deletedObjects) != 1 {
		t.Fatalf("deleted objects = %d, want specialized VAP", len(writer.deletedObjects))
	}
	if _, ok := writer.deletedObjects[0].(*admissionregistrationv1.ValidatingAdmissionPolicy); !ok {
		t.Fatalf("deleted object = %T, want VAP", writer.deletedObjects[0])
	}
}

type migrationWriter struct {
	*trackingWriter
	events []string
}

func (writer *migrationWriter) Update(ctx context.Context, object client.Object, options ...client.UpdateOption) error {
	if err := writer.trackingWriter.Update(ctx, object, options...); err != nil {
		return err
	}
	writer.events = append(writer.events, "update "+object.GetName())
	return nil
}

func (writer *migrationWriter) Delete(ctx context.Context, object client.Object, options ...client.DeleteOption) error {
	if err := writer.trackingWriter.Delete(ctx, object, options...); err != nil {
		return err
	}
	writer.events = append(writer.events, "delete "+object.GetName())
	delete(writer.reader.objects, client.ObjectKeyFromObject(object))
	return nil
}

func TestManageVAPB_ModeMigration(t *testing.T) {
	for _, version := range []string{vapAPIVersionV1, vapAPIVersionV1Beta1} {
		for _, rollback := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/rollback=%t", version, rollback), func(t *testing.T) {
				initialMode, targetMode := VAPGenerationModeTemplate, VAPGenerationModeConstraint
				if rollback {
					initialMode, targetMode = targetMode, initialMode
				}
				configureVAP(t, vapTestConfig{
					apiEnabled: ptr.To(true), defaultGenerateVAP: ptr.To(true),
					defaultGenerateVAPB: ptr.To(true), generationMode: &initialMode,
				})
				groupVersion := schema.GroupVersion{Group: admissionregistrationv1.GroupName, Version: version}
				transform.SetGroupVersion(&groupVersion)
				template := makeUnitCELTemplate()
				template.SetAnnotations(map[string]string{VAPBGenerationAnnotation: VAPBGenerationUnblocked})
				instance := makeUnitConstraint()
				reconciler, reader, tracking, _ := newConstraintUnitReconciler(t, template, instance)
				writer := &migrationWriter{trackingWriter: tracking}
				reconciler.writer = writer
				sharedName := transform.GetTemplateVAPName(instance.GetKind())
				inlineName := transform.GetConstraintVAPName(instance.GetKind(), instance.GetName())
				bindingKey := types.NamespacedName{Name: transform.GetVAPBindingName(instance.GetKind(), instance.GetName())}
				sharedPolicy := makeUnitTemplateVAP(t, template, &groupVersion)
				reader.objects[types.NamespacedName{Name: sharedName}] = sharedPolicy
				if delay, err := reconciler.manageVAPB(context.Background(), util.Dryrun, instance, &constraintstatusv1beta1.ConstraintPodStatus{}); err != nil || delay != 0 {
					t.Fatalf("initial generation: delay=%s, err=%v", delay, err)
				}
				reader.objects[bindingKey].SetUID("stable-binding")
				reader.objects[bindingKey].SetResourceVersion("1")
				writer.updatedObjects, writer.deletedObjects, writer.events = nil, nil, nil
				if err := SetVAPGenerationMode(targetMode); err != nil {
					t.Fatal(err)
				}
				before := reader.objects[bindingKey].DeepCopyObject()
				updateErr := errors.New("binding update failed")
				writer.updateErr = updateErr
				if _, err := reconciler.manageVAPB(context.Background(), util.Dryrun, instance, &constraintstatusv1beta1.ConstraintPodStatus{}); !errors.Is(err, updateErr) {
					t.Fatalf("migration update error = %v, want %v", err, updateErr)
				}
				if len(writer.events) != 0 || !reflect.DeepEqual(before, reader.objects[bindingKey]) || reader.objects[types.NamespacedName{Name: inlineName}] == nil {
					t.Fatal("failed binding update changed the binding or deleted its policy")
				}
				writer.updateErr = nil
				status := &constraintstatusv1beta1.ConstraintPodStatus{}
				if delay, err := reconciler.manageVAPB(context.Background(), util.Dryrun, instance, status); err != nil || delay != 0 {
					t.Fatalf("migration: delay=%s, err=%v", delay, err)
				}
				wantPolicy := inlineName
				wantEvents := []string{"update " + bindingKey.Name}
				if rollback {
					wantPolicy = sharedName
					wantEvents = append(wantEvents, "delete "+inlineName)
				}
				if !reflect.DeepEqual(writer.events, wantEvents) {
					t.Fatalf("migration writes = %v, want %v", writer.events, wantEvents)
				}
				stored := reader.objects[bindingKey]
				var policyName string
				var hasParameters bool
				switch binding := stored.(type) {
				case *admissionregistrationv1.ValidatingAdmissionPolicyBinding:
					policyName, hasParameters = binding.Spec.PolicyName, binding.Spec.ParamRef != nil
				case *admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding:
					policyName, hasParameters = binding.Spec.PolicyName, binding.Spec.ParamRef != nil
				default:
					t.Fatalf("unexpected binding %T", stored)
				}
				if policyName != wantPolicy || hasParameters != rollback || reader.objects[types.NamespacedName{Name: policyName}] == nil {
					t.Fatalf("stored binding reference = %s, params=%t; want existing %s, params=%t", policyName, hasParameters, wantPolicy, rollback)
				}
				if stored.GetUID() != "stable-binding" || stored.GetResourceVersion() != "1" || !metav1.IsControlledBy(stored, instance) {
					t.Fatal("migration changed binding identity or ownership")
				}
				writer.events = nil
				if _, err := reconciler.manageVAPB(context.Background(), util.Dryrun, instance, status); err != nil {
					t.Fatal(err)
				}
				if len(writer.events) != 0 {
					t.Fatalf("repeat reconcile wrote objects: %v", writer.events)
				}
			})
		}
	}
}

func makeUnitTemplateVAP(t *testing.T, template *templates.ConstraintTemplate, groupVersion *schema.GroupVersion) client.Object {
	t.Helper()
	policy, err := transform.TemplateToPolicyDefinition(template)
	if err != nil {
		t.Fatal(err)
	}
	policy.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: templatesv1beta1.SchemeGroupVersion.String(), Kind: "ConstraintTemplate",
		Name: template.GetName(), UID: template.GetUID(), Controller: ptr.To(true),
	}})
	result, err := getRunTimeVAP(groupVersion, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestGetRunTimeVAPBindingPreservesCurrent(t *testing.T) {
	for _, version := range []string{vapAPIVersionV1, vapAPIVersionV1Beta1} {
		t.Run(version, func(t *testing.T) {
			groupVersion := schema.GroupVersion{Group: admissionregistrationv1.GroupName, Version: version}
			current, err := vapBindingForVersion(groupVersion)
			if err != nil {
				t.Fatal(err)
			}
			current.SetName("binding")
			current.SetResourceVersion("1")
			before := current.DeepCopyObject()
			desired := &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{
				Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicyBindingSpec{PolicyName: "new-policy"},
			}
			proposed, err := getRunTimeVAPBinding(&groupVersion, desired, current)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(current, before) || reflect.DeepEqual(current, proposed) {
				t.Fatal("binding conversion mutated the current object or suppressed the update")
			}
		})
	}
}

func TestV1beta1VAPToV1WithoutParamKind(t *testing.T) {
	converted, err := v1beta1VAPToV1(&admissionregistrationv1beta1.ValidatingAdmissionPolicy{
		Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicySpec{
			FailurePolicy: ptr.To(admissionregistrationv1beta1.Fail),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if converted.Spec.ParamKind != nil {
		t.Fatalf("ParamKind = %#v, want nil", converted.Spec.ParamKind)
	}
}

func TestV1beta1ToV1WithoutParamRef(t *testing.T) {
	converted, err := v1beta1ToV1(&admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{
		Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicyBindingSpec{
			PolicyName:        "inlined-policy",
			ValidationActions: []admissionregistrationv1beta1.ValidationAction{admissionregistrationv1beta1.Deny},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if converted.Spec.ParamRef != nil {
		t.Fatalf("ParamRef = %#v, want nil", converted.Spec.ParamRef)
	}
}

func TestDeleteConstraintVAPIfOwnedSkipsUnrelatedPolicy(t *testing.T) {
	configureVAP(t, vapTestConfig{apiEnabled: ptr.To(true)})
	ct := makeUnitCELTemplate()
	instance := makeUnitConstraint()
	r, reader, writer, _ := newConstraintUnitReconciler(t, ct, instance)
	policy := &admissionregistrationv1.ValidatingAdmissionPolicy{ObjectMeta: metav1.ObjectMeta{
		Name: transform.GetConstraintVAPName(instance.GetKind(), instance.GetName()),
	}}
	reader.objects[client.ObjectKeyFromObject(policy)] = policy

	if err := r.deleteConstraintVAPIfOwned(context.Background(), instance, &admissionregistrationv1.SchemeGroupVersion); err != nil {
		t.Fatal(err)
	}
	if len(writer.deletedObjects) != 0 {
		t.Fatalf("deleted %d unrelated policies", len(writer.deletedObjects))
	}
}

type recordingDeleteWriter struct {
	client.Writer
	options []client.DeleteOptions
}

func (writer *recordingDeleteWriter) Delete(ctx context.Context, object client.Object, options ...client.DeleteOption) error {
	deleteOptions := client.DeleteOptions{}
	deleteOptions.ApplyOptions(options)
	writer.options = append(writer.options, deleteOptions)
	return writer.Writer.Delete(ctx, object, options...)
}

func TestDeleteConstraintResourcesPreservesReplacement(t *testing.T) {
	const (
		vapResource = "VAP"
		unchanged   = "unchanged"
	)
	for _, version := range []string{vapAPIVersionV1, vapAPIVersionV1Beta1} {
		for _, resource := range []string{vapResource, "VAPB", "legacy VAPB"} {
			for _, changed := range []string{"replacement", "ownership", unchanged} {
				t.Run(fmt.Sprintf("%s/%s/%s", version, resource, changed), func(t *testing.T) {
					instance := makeUnitConstraint()
					reconciler, reader, _, _ := newConstraintUnitReconciler(t, makeUnitCELTemplate(), instance)
					groupVersion := schema.GroupVersion{Group: admissionregistrationv1.GroupName, Version: version}
					var cached client.Object
					var err error
					if resource == vapResource {
						cached, err = vapForVersion(&groupVersion)
					} else {
						cached, err = vapBindingForVersion(groupVersion)
					}
					if err != nil {
						t.Fatal(err)
					}
					switch resource {
					case vapResource:
						cached.SetName(transform.GetConstraintVAPName(instance.GetKind(), instance.GetName()))
					case "VAPB":
						cached.SetName(transform.GetVAPBindingName(instance.GetKind(), instance.GetName()))
					case "legacy VAPB":
						cached.SetName(transform.LegacyVAPBindingName(instance.GetName()))
					}
					cached.SetUID("original-resource")
					cached.SetResourceVersion("1")
					cached.SetOwnerReferences([]metav1.OwnerReference{{
						APIVersion: instance.GetAPIVersion(), Kind: instance.GetKind(), Name: instance.GetName(),
						UID: instance.GetUID(), Controller: ptr.To(true),
					}})
					reader.objects[client.ObjectKeyFromObject(cached)] = cached
					live, ok := cached.DeepCopyObject().(client.Object)
					if !ok {
						t.Fatalf("copy of %T does not implement client.Object", cached)
					}
					if changed != unchanged {
						live.SetResourceVersion("2")
						owners := live.GetOwnerReferences()
						owners[0].UID = "new-constraint"
						live.SetOwnerReferences(owners)
					}
					if changed == "replacement" {
						live.SetUID("replacement-resource")
					}
					liveClient := crfake.NewClientBuilder().WithScheme(reconciler.scheme).WithObjects(live).Build()
					writer := &recordingDeleteWriter{Writer: liveClient}
					reconciler.writer = writer
					deleteResource := func() error {
						switch resource {
						case vapResource:
							return reconciler.deleteConstraintVAPIfOwned(context.Background(), instance, &groupVersion)
						case "VAPB":
							return reconciler.deleteVAPBIfOwned(context.Background(), reader.objects[client.ObjectKeyFromObject(cached)], instance, cached.GetName())
						default:
							return reconciler.cleanupLegacyVAPB(context.Background(), instance, &groupVersion)
						}
					}
					err = deleteResource()
					if changed != unchanged && !apierrors.IsConflict(err) {
						t.Fatalf("delete error = %v, want precondition conflict", err)
					}
					if changed == unchanged && err != nil {
						t.Fatal(err)
					}
					if len(writer.options) != 1 {
						t.Fatalf("delete attempts = %d, want 1", len(writer.options))
					}
					preconditions := writer.options[0].Preconditions
					if preconditions == nil || preconditions.UID == nil || *preconditions.UID != cached.GetUID() || preconditions.ResourceVersion == nil || *preconditions.ResourceVersion != cached.GetResourceVersion() {
						t.Fatalf("delete preconditions = %#v, want observed UID and resource version", preconditions)
					}
					stored, ok := live.DeepCopyObject().(client.Object)
					if !ok {
						t.Fatalf("copy of %T does not implement client.Object", live)
					}
					getErr := liveClient.Get(context.Background(), client.ObjectKeyFromObject(live), stored)
					if changed == unchanged {
						if !apierrors.IsNotFound(getErr) {
							t.Fatalf("owned resource remains after delete: %v", getErr)
						}
						return
					}
					if getErr != nil {
						t.Fatalf("replacement was deleted: %v", getErr)
					}
					reader.objects[client.ObjectKeyFromObject(live)] = stored
					if err := deleteResource(); err != nil || len(writer.options) != 1 {
						t.Fatalf("retry did not skip the new owner: error=%v, deletes=%d", err, len(writer.options))
					}
				})
			}
		}
	}
}

func TestReconcileDeletedConstraintCleansChildrenWithoutTemplateEligibility(t *testing.T) {
	for _, mode := range []VAPGenerationMode{VAPGenerationModeTemplate, VAPGenerationModeConstraint} {
		for _, version := range []string{vapAPIVersionV1, vapAPIVersionV1Beta1} {
			for _, state := range []string{"template absent", "generation disabled"} {
				t.Run(fmt.Sprintf("%s/%s/%s", mode, version, state), func(t *testing.T) {
					configureVAP(t, vapTestConfig{
						apiEnabled: ptr.To(true), defaultGenerateVAP: ptr.To(true), defaultGenerateVAPB: ptr.To(true),
						generationMode: &mode,
					})
					groupVersion := schema.GroupVersion{Group: admissionregistrationv1.GroupName, Version: version}
					transform.SetGroupVersion(&groupVersion)
					template := makeUnitCELTemplate()
					template.SetAnnotations(map[string]string{VAPBGenerationAnnotation: VAPBGenerationUnblocked})
					instance := makeUnitConstraint()
					reconciler, reader, tracking, request := newConstraintUnitReconciler(t, template, instance)
					sharedKey := types.NamespacedName{Name: transform.GetTemplateVAPName(instance.GetKind())}
					reader.objects[sharedKey] = makeUnitTemplateVAP(t, template, &groupVersion)
					if _, err := reconciler.manageVAPB(context.Background(), util.Dryrun, instance, &constraintstatusv1beta1.ConstraintPodStatus{}); err != nil {
						t.Fatal(err)
					}
					bindingKey := types.NamespacedName{Name: transform.GetVAPBindingName(instance.GetKind(), instance.GetName())}
					legacy, ok := reader.objects[bindingKey].DeepCopyObject().(client.Object)
					if !ok {
						t.Fatalf("copy of %T does not implement client.Object", reader.objects[bindingKey])
					}
					legacy.SetName(transform.LegacyVAPBindingName(instance.GetName()))
					reader.objects[client.ObjectKeyFromObject(legacy)] = legacy
					delete(reader.objects, client.ObjectKeyFromObject(instance))
					if state == "template absent" {
						delete(reader.objects, types.NamespacedName{Name: template.GetName()})
						if _, err := reconciler.cfClient.RemoveTemplate(context.Background(), template); err != nil {
							t.Fatal(err)
						}
						reconciler.ifWatching = func(schema.GroupVersionKind, func() error) (bool, error) { return false, nil }
					} else {
						SetDefaultGenerateVAP(false)
						SetDefaultGenerateVAPB(false)
					}
					reconciler.writer = &migrationWriter{trackingWriter: tracking}
					for attempt := 0; attempt < 2; attempt++ {
						if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
							t.Fatal(err)
						}
						for _, name := range []string{bindingKey.Name, legacy.GetName(), transform.GetConstraintVAPName(instance.GetKind(), instance.GetName())} {
							if _, found := reader.objects[types.NamespacedName{Name: name}]; found {
								t.Fatalf("generated resource %s remains after deletion", name)
							}
						}
						if _, found := reader.objects[sharedKey]; !found {
							t.Fatal("constraint cleanup deleted the shared template policy")
						}
					}
				})
			}
		}
	}
}

func makeUnitCELTemplate() *templates.ConstraintTemplate {
	ct := makeTemplateWithCELEngine(nil)
	ct.Spec.CRD.Spec.Names.Kind = "TestKind"
	ct.Spec.Targets[0].Target = target.Name
	return ct
}

func TestReconcileStableVAPAPIErrorSkipsStatusUpdate(t *testing.T) {
	configureVAP(t, vapTestConfig{
		apiEnabled:          ptr.To(false),
		defaultGenerateVAP:  ptr.To(true),
		defaultGenerateVAPB: ptr.To(true),
	})

	ct := makeUnitCELTemplate()
	instance := makeUnitConstraint()
	r, reader, writer, request := newConstraintUnitReconciler(t, ct, instance)

	firstResult, firstErr := r.Reconcile(context.Background(), request)
	if firstErr != nil || firstResult != (reconcile.Result{}) {
		t.Fatalf("expected successful first reconcile, got result=%v error=%v", firstResult, firstErr)
	}
	if writer.createAttempts != 1 || writer.updateAttempts != 1 {
		t.Fatalf("expected one status create and update, got %d creates and %d updates", writer.createAttempts, writer.updateAttempts)
	}

	secondResult, secondErr := r.Reconcile(context.Background(), request)
	if secondErr != nil || secondResult != firstResult {
		t.Fatalf("expected stable second reconcile, got result=%v error=%v", secondResult, secondErr)
	}
	if writer.createAttempts != 1 || writer.updateAttempts != 1 {
		t.Fatalf("expected identical error to skip a second write, got %d creates and %d updates", writer.createAttempts, writer.updateAttempts)
	}

	statusName, err := constraintstatusv1beta1.KeyForConstraint("test-pod", instance)
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := reader.objects[types.NamespacedName{Name: statusName, Namespace: util.GetNamespace()}].(*constraintstatusv1beta1.ConstraintPodStatus)
	if !ok {
		t.Fatalf("expected stored ConstraintPodStatus, got %T", reader.objects[types.NamespacedName{Name: statusName, Namespace: util.GetNamespace()}])
	}
	if len(stored.Status.Errors) != 1 || !strings.Contains(stored.Status.Errors[0].Message, ErrValidatingAdmissionPolicyAPIDisabled.Error()) {
		t.Fatalf("expected one stable VAP API error, got %v", stored.Status.Errors)
	}
}

type recreatedConstraintReader struct {
	client.Reader
	key    types.NamespacedName
	missed bool
}

func (reader *recreatedConstraintReader) Get(ctx context.Context, key types.NamespacedName, object client.Object, options ...client.GetOption) error {
	if key == reader.key && !reader.missed {
		reader.missed = true
		return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
	}
	return reader.Reader.Get(ctx, key, object, options...)
}

func TestReconcileConstraintRecreatedAfterAbsenceCheck(t *testing.T) {
	for _, mode := range []VAPGenerationMode{VAPGenerationModeTemplate, VAPGenerationModeConstraint} {
		for _, version := range []string{vapAPIVersionV1, vapAPIVersionV1Beta1} {
			t.Run(fmt.Sprintf("%s/%s", mode, version), func(t *testing.T) {
				configureVAP(t, vapTestConfig{
					apiEnabled: ptr.To(true), defaultGenerateVAP: ptr.To(true), defaultGenerateVAPB: ptr.To(true),
					generationMode: &mode,
				})
				groupVersion := schema.GroupVersion{Group: admissionregistrationv1.GroupName, Version: version}
				transform.SetGroupVersion(&groupVersion)
				template := makeUnitCELTemplate()
				template.SetAnnotations(map[string]string{VAPBGenerationAnnotation: VAPBGenerationUnblocked})
				instance := makeUnitConstraint()
				reconciler, reader, writer, request := newConstraintUnitReconciler(t, template, instance)
				reader.objects[types.NamespacedName{Name: transform.GetTemplateVAPName(instance.GetKind())}] = makeUnitTemplateVAP(t, template, &groupVersion)
				if _, err := reconciler.manageVAPB(context.Background(), util.Dryrun, instance, &constraintstatusv1beta1.ConstraintPodStatus{}); err != nil {
					t.Fatal(err)
				}
				bindingKey := types.NamespacedName{Name: transform.GetVAPBindingName(instance.GetKind(), instance.GetName())}
				legacy, ok := reader.objects[bindingKey].DeepCopyObject().(client.Object)
				if !ok {
					t.Fatalf("copy of %T does not implement client.Object", reader.objects[bindingKey])
				}
				legacy.SetName(transform.LegacyVAPBindingName(instance.GetName()))
				reader.objects[client.ObjectKeyFromObject(legacy)] = legacy
				key := client.ObjectKeyFromObject(instance)
				reader.getErrs[key] = apierrors.NewNotFound(schema.GroupResource{}, instance.GetName())
				reconciler.apiReader = &recreatedConstraintReader{Reader: &fakeReader{objects: reader.objects}, key: key}
				if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
					t.Fatal(err)
				}
				for _, deleted := range writer.deletedObjects {
					if _, status := deleted.(*constraintstatusv1beta1.ConstraintPodStatus); !status {
						t.Fatalf("deleted resource %s after its owner was recreated", deleted.GetName())
					}
				}
			})
		}
	}
}

func TestReconcileCachedMissUsesAPIReaderBeforeDeletingChildren(t *testing.T) {
	const (
		liveState        = "live"
		deletingState    = "deleting"
		absentState      = "absent"
		lookupErrorState = "lookup error"
		noAPIReaderState = "no API reader"
	)
	for _, mode := range []VAPGenerationMode{VAPGenerationModeTemplate, VAPGenerationModeConstraint} {
		for _, version := range []string{vapAPIVersionV1, vapAPIVersionV1Beta1} {
			t.Run(fmt.Sprintf("%s/%s", mode, version), func(t *testing.T) {
				configureVAP(t, vapTestConfig{
					apiEnabled: ptr.To(true), defaultGenerateVAP: ptr.To(true), defaultGenerateVAPB: ptr.To(true),
					generationMode: &mode,
				})
				groupVersion := schema.GroupVersion{Group: admissionregistrationv1.GroupName, Version: version}
				transform.SetGroupVersion(&groupVersion)
				for _, state := range []string{liveState, deletingState, absentState, lookupErrorState, noAPIReaderState} {
					t.Run(state, func(t *testing.T) {
						template := makeUnitCELTemplate()
						template.SetAnnotations(map[string]string{VAPBGenerationAnnotation: VAPBGenerationUnblocked})
						instance := makeUnitConstraint()
						reconciler, reader, writer, request := newConstraintUnitReconciler(t, template, instance)
						reader.objects[types.NamespacedName{Name: transform.GetTemplateVAPName(instance.GetKind())}] = makeUnitTemplateVAP(t, template, &groupVersion)
						if _, err := reconciler.manageVAPB(context.Background(), util.Dryrun, instance, &constraintstatusv1beta1.ConstraintPodStatus{}); err != nil {
							t.Fatal(err)
						}
						key := client.ObjectKeyFromObject(instance)
						reader.getErrs[key] = apierrors.NewNotFound(schema.GroupResource{}, instance.GetName())
						apiReader := &fakeReader{objects: map[types.NamespacedName]client.Object{key: instance.DeepCopy()}}
						reconciler.apiReader = apiReader
						lookupErr := errors.New("constraint lookup failed")
						switch state {
						case deletingState:
							now := metav1.Now()
							apiReader.objects[key].SetDeletionTimestamp(&now)
						case absentState:
							delete(apiReader.objects, key)
						case lookupErrorState:
							apiReader.getErr = lookupErr
						case noAPIReaderState:
							reconciler.apiReader = nil
						}
						writer.createdObjects, writer.updatedObjects, writer.deletedObjects = nil, nil, nil
						result, err := reconciler.Reconcile(context.Background(), request)
						switch state {
						case lookupErrorState:
							if !errors.Is(err, lookupErr) {
								t.Fatalf("error = %v, want %v", err, lookupErr)
							}
						case noAPIReaderState:
							if err == nil {
								t.Fatal("expected missing API reader error")
							}
						default:
							if err != nil {
								t.Fatal(err)
							}
						}
						if state == liveState && result.RequeueAfter == 0 {
							t.Fatal("live constraint must requeue until its cache catches up")
						}
						wantDelete := state == absentState || state == deletingState
						if (len(writer.deletedObjects) > 0) != wantDelete {
							t.Fatalf("deletes = %d, want deletion = %v", len(writer.deletedObjects), wantDelete)
						}
						if len(writer.createdObjects) != 0 || len(writer.updatedObjects) != 0 {
							t.Fatal("cached miss must not create or update resources")
						}
					})
				}
			})
		}
	}
}

func TestReconcileNotWatchingUsesAPIReaderBeforeDeletingChildren(t *testing.T) {
	configureVAP(t, vapTestConfig{
		apiEnabled:          ptr.To(true),
		defaultGenerateVAP:  ptr.To(true),
		defaultGenerateVAPB: ptr.To(true),
		generationMode:      ptr.To(VAPGenerationModeConstraint),
	})

	lookupError := errors.New("template lookup failed")
	for _, state := range []string{"live", "absent", "deleting", "lookup error"} {
		t.Run(state, func(t *testing.T) {
			ct := makeUnitCELTemplate()
			instance := makeUnitConstraint()
			r, reader, writer, request := newConstraintUnitReconciler(t, ct, instance)
			if _, err := r.manageVAPB(context.Background(), util.Dryrun, instance, &constraintstatusv1beta1.ConstraintPodStatus{}); err != nil {
				t.Fatal(err)
			}
			if _, err := r.cfClient.RemoveTemplate(context.Background(), ct); err != nil {
				t.Fatal(err)
			}
			templateKey := types.NamespacedName{Name: ct.GetName()}
			switch state {
			case "absent":
				delete(reader.objects, templateKey)
				delete(reader.objects, client.ObjectKeyFromObject(instance))
			case "deleting":
				now := metav1.Now()
				reader.objects[templateKey].SetDeletionTimestamp(&now)
				reader.objects[client.ObjectKeyFromObject(instance)].SetDeletionTimestamp(&now)
			case "lookup error":
				reader.getErrs[templateKey] = lookupError
			}
			writer.createdObjects = nil
			writer.updatedObjects = nil
			writer.deletedObjects = nil
			r.ifWatching = func(_ schema.GroupVersionKind, _ func() error) (bool, error) {
				return false, nil
			}
			result, err := r.Reconcile(context.Background(), request)
			if state == "lookup error" {
				if !errors.Is(err, lookupError) {
					t.Fatalf("error = %v, want %v", err, lookupError)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(writer.createdObjects) != 0 || len(writer.updatedObjects) != 0 {
				t.Fatal("reconciled Constraint before its template watch was registered")
			}
			if state == "live" && result.RequeueAfter != time.Second {
				t.Fatalf("result = %v, want retry for watch registration", result)
			}
			wantDelete := state == "absent" || state == "deleting"
			if (len(writer.deletedObjects) > 0) != wantDelete {
				t.Fatalf("deleted objects = %d, want deletion = %v", len(writer.deletedObjects), wantDelete)
			}
		})
	}
}

func TestReconcileStatusCreateErrorIsReturned(t *testing.T) {
	ct := makeUnitCELTemplate()
	instance := makeUnitConstraint()
	r, _, writer, request := newConstraintUnitReconciler(t, ct, instance)
	statusName, err := constraintstatusv1beta1.KeyForConstraint("test-pod", instance)
	if err != nil {
		t.Fatal(err)
	}
	createErr := apierrors.NewAlreadyExists(schema.GroupResource{Group: constraintstatusv1beta1.GroupVersion.Group, Resource: "constraintpodstatuses"}, statusName)
	writer.createErr = createErr

	result, err := r.Reconcile(context.Background(), request)
	if !apierrors.IsAlreadyExists(err) {
		t.Fatalf("expected status create error %v, got %v", createErr, err)
	}
	if result != (reconcile.Result{}) {
		t.Fatalf("expected empty result for status create error, got %v", result)
	}
	if writer.createAttempts != 1 || writer.updateAttempts != 0 {
		t.Fatalf("expected one create and no updates, got %d creates and %d updates", writer.createAttempts, writer.updateAttempts)
	}
}

func TestReconcileBaseStatusUpdateErrorPreservesRequeueBehavior(t *testing.T) {
	configureVAP(t, vapTestConfig{
		apiEnabled:          ptr.To(false),
		defaultGenerateVAP:  ptr.To(true),
		defaultGenerateVAPB: ptr.To(true),
	})
	ct := makeUnitCELTemplate()
	instance := makeUnitConstraint()
	r, _, writer, request := newConstraintUnitReconciler(t, ct, instance)
	updateErr := apierrors.NewConflict(schema.GroupResource{Group: constraintstatusv1beta1.GroupVersion.Group, Resource: "constraintpodstatuses"}, instance.GetName(), errors.New("conflict"))
	writer.updateErr = updateErr

	result, err := r.Reconcile(context.Background(), request)
	if err != nil {
		t.Fatalf("expected status update failure to preserve nil error, got %v", err)
	}
	if result != (reconcile.Result{Requeue: true}) {
		t.Fatalf("expected explicit requeue for base status update failure, got %v", result)
	}
	if writer.createAttempts != 1 || writer.updateAttempts != 1 {
		t.Fatalf("expected one create and one update attempt, got %d creates and %d updates", writer.createAttempts, writer.updateAttempts)
	}
}

func TestReconcilePreservesBothVAPBAndStatusErrors(t *testing.T) {
	configureVAP(t, vapTestConfig{
		apiEnabled:          ptr.To(true),
		defaultGenerateVAP:  ptr.To(true),
		defaultGenerateVAPB: ptr.To(true),
	})

	ct := makeUnitCELTemplate()
	ct.SetAnnotations(map[string]string{VAPBGenerationAnnotation: VAPBGenerationUnblocked})
	instance := makeUnitConstraint()
	r, reader, writer, request := newConstraintUnitReconciler(t, ct, instance)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: util.GetNamespace()}}
	status, err := constraintstatusv1beta1.NewConstraintStatusForPod(pod, instance, r.scheme)
	if err != nil {
		t.Fatal(err)
	}
	status.Status.ConstraintUID = instance.GetUID()
	status.Status.ObservedGeneration = instance.GetGeneration()
	status.Status.Enforced = true
	writer.store(status)
	reader.objects[types.NamespacedName{Name: transform.GetTemplateVAPName(instance.GetKind())}] = &admissionregistrationv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: transform.GetTemplateVAPName(instance.GetKind())},
	}

	reconcileErr := errors.New("get VAPB")
	persistErr := apierrors.NewConflict(schema.GroupResource{Group: constraintstatusv1beta1.GroupVersion.Group, Resource: "constraintpodstatuses"}, instance.GetName(), errors.New("conflict"))
	reader.getErrs[types.NamespacedName{Name: transform.GetVAPBindingName(instance.GetKind(), instance.GetName())}] = reconcileErr
	writer.statusUpdateErr = persistErr
	writer.statusUpdateErrOnlyOnErrors = true

	result, err := r.Reconcile(context.Background(), request)
	if result != (reconcile.Result{}) {
		t.Fatalf("expected empty result for combined error, got %v", result)
	}
	if !errors.Is(err, reconcileErr) {
		t.Fatalf("expected combined error to preserve VAPB error %v, got %v", reconcileErr, err)
	}
	if !errors.Is(err, persistErr) {
		t.Fatalf("expected combined error to preserve status error %v, got %v", persistErr, err)
	}
	wantMessage := fmt.Sprintf("could not get ValidatingAdmissionPolicyBinding, could not update constraint status: %s: %s", persistErr, reconcileErr)
	if err.Error() != wantMessage {
		t.Fatalf("expected unchanged combined error message %q, got %q", wantMessage, err)
	}
	if writer.createAttempts != 0 || writer.updateAttempts != 1 {
		t.Fatalf("expected no create and one update attempt, got %d creates and %d updates", writer.createAttempts, writer.updateAttempts)
	}
}

func TestManageVAPB_CleansUpStaleVAPB(t *testing.T) {
	// Regression test for https://github.com/open-policy-agent/gatekeeper/issues/4441
	// When vap.k8s.io is removed from scopedEnforcementActions, the stale VAPB must be deleted.

	configureVAP(t, vapTestConfig{
		apiEnabled:          ptr.To(true),
		defaultGenerateVAPB: ptr.To(true),
	})

	// Constraint with scoped enforcement — only webhook + audit, no vap.k8s.io.
	instance := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "TestKind",
			"metadata": map[string]interface{}{
				"name": "test-constraint",
				"uid":  "12345",
			},
			"spec": map[string]interface{}{
				"enforcementAction": "scoped",
				"scopedEnforcementActions": []interface{}{
					map[string]interface{}{
						"action": "deny",
						"enforcementPoints": []interface{}{
							map[string]interface{}{"name": util.WebhookEnforcementPoint},
							map[string]interface{}{"name": util.AuditEnforcementPoint},
						},
					},
				},
			},
		},
	}

	// A stale VAPB owned by this constraint — should be cleaned up.
	staleVAPB := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gatekeeper-testkind-test-constraint",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "constraints.gatekeeper.sh/v1beta1",
				Kind:       "TestKind",
				Name:       "test-constraint",
				UID:        "12345",
				Controller: ptr.To(true),
			}},
		},
	}

	// A minimal ConstraintTemplate (needed because manageVAPB does reader.Get for it).
	ct := &templatesv1beta1.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testkind",
		},
	}

	reader := &fakeReader{
		objects: map[types.NamespacedName]client.Object{
			{Name: "testkind"}: ct,
			{Name: "gatekeeper-testkind-test-constraint"}: staleVAPB,
		},
	}

	writer := &trackingWriter{}

	status := &constraintstatusv1beta1.ConstraintPodStatus{
		Status: constraintstatusv1beta1.ConstraintPodStatusStatus{},
	}

	r := &ReconcileConstraint{
		reader:   reader,
		writer:   writer,
		log:      logf.Log.WithName("test"),
		reporter: &fakeReporter{},
		scheme:   runtime.NewScheme(),
	}

	_, err := r.manageVAPB(context.Background(), util.Scoped, instance, status)
	if err != nil {
		t.Fatalf("manageVAPB returned unexpected error: %v", err)
	}

	if len(writer.deletedObjects) != 1 {
		t.Fatalf("expected 1 VAPB to be deleted, got %d", len(writer.deletedObjects))
	}

	deletedVAPB, ok := writer.deletedObjects[0].(*admissionregistrationv1.ValidatingAdmissionPolicyBinding)
	if !ok {
		t.Fatalf("deleted object is not a ValidatingAdmissionPolicyBinding, got %T", writer.deletedObjects[0])
	}
	if deletedVAPB.Name != "gatekeeper-testkind-test-constraint" {
		t.Errorf("expected deleted VAPB name 'gatekeeper-testkind-test-constraint', got %q", deletedVAPB.Name)
	}
	if len(writer.updatedObjects) != 0 {
		t.Fatalf("expected no status updates when stale VAPB cleanup leaves status unchanged, got %d", len(writer.updatedObjects))
	}
}

func TestManageVAPB_RegoOnlyTemplateSkipsVAPAPIDisabledError(t *testing.T) {
	configureVAP(t, vapTestConfig{
		apiEnabled:          ptr.To(false),
		defaultGenerateVAP:  ptr.To(true),
		defaultGenerateVAPB: ptr.To(true),
	})

	scheme := runtime.NewScheme()
	if err := templatesv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	ct := &templatesv1beta1.ConstraintTemplate{}
	if err := scheme.Convert(makeTemplateWithRegoEngine(), ct, nil); err != nil {
		t.Fatal(err)
	}

	instance := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "TestKind",
			"metadata": map[string]interface{}{
				"generation": int64(1),
				"name":       "test-constraint",
				"uid":        "12345",
			},
			"spec": map[string]interface{}{
				"enforcementAction": "dryrun",
			},
		},
	}

	writer := &trackingWriter{}
	status := &constraintstatusv1beta1.ConstraintPodStatus{
		Status: constraintstatusv1beta1.ConstraintPodStatusStatus{
			Errors: []constraintstatusv1beta1.Error{{
				Message: fmt.Sprintf("cannot generate ValidatingAdmissionPolicyBinding: %s", ErrValidatingAdmissionPolicyAPIDisabled),
			}},
		},
	}
	r := &ReconcileConstraint{
		reader: &fakeReader{
			objects: map[types.NamespacedName]client.Object{
				{Name: "testkind"}: ct,
			},
		},
		writer:   writer,
		log:      logf.Log.WithName("test"),
		reporter: &fakeReporter{},
		scheme:   scheme,
	}

	oldStatus := status.Status.DeepCopy()
	status.Status.Errors = nil
	requeueAfter, err := r.manageVAPB(context.Background(), util.Dryrun, instance, status)
	if err != nil {
		t.Fatalf("manageVAPB returned unexpected error: %v", err)
	}
	if requeueAfter != 0 {
		t.Fatalf("expected no requeue delay, got %s", requeueAfter)
	}
	if len(status.Status.Errors) != 0 {
		t.Fatalf("expected no VAP API error for Rego-only template, got %v", status.Status.Errors)
	}
	if len(status.Status.EnforcementPointsStatus) != 1 {
		t.Fatalf("expected one enforcement point status, got %d", len(status.Status.EnforcementPointsStatus))
	}
	if got := status.Status.EnforcementPointsStatus[0]; got.EnforcementPoint != util.VAPEnforcementPoint || got.State != ErrGenerateVAPBState {
		t.Fatalf("expected VAP enforcement point state %q, got %#v", ErrGenerateVAPBState, got)
	}
	if len(writer.updatedObjects) != 0 {
		t.Fatalf("expected manageVAPB to leave status persistence to Reconcile, got %d updates", len(writer.updatedObjects))
	}
	if err := r.persistPodStatus(context.Background(), status, oldStatus); err != nil {
		t.Fatalf("persistPodStatus returned unexpected error: %v", err)
	}
	if len(writer.updatedObjects) != 1 {
		t.Fatalf("expected one update to replace the stale API error, got %d", len(writer.updatedObjects))
	}

	oldStatus = status.Status.DeepCopy()
	status.Status.Errors = nil
	if _, err := r.manageVAPB(context.Background(), util.Dryrun, instance, status); err != nil {
		t.Fatalf("second manageVAPB returned unexpected error: %v", err)
	}
	if err := r.persistPodStatus(context.Background(), status, oldStatus); err != nil {
		t.Fatalf("second persistPodStatus returned unexpected error: %v", err)
	}
	if len(writer.updatedObjects) != 1 {
		t.Fatalf("expected stable status to skip a second update, got %d updates", len(writer.updatedObjects))
	}

	celCT := &templatesv1beta1.ConstraintTemplate{}
	if err := scheme.Convert(makeTemplateWithCELEngine(nil), celCT, nil); err != nil {
		t.Fatal(err)
	}
	reader, ok := r.reader.(*fakeReader)
	if !ok {
		t.Fatalf("expected fake reader, got %T", r.reader)
	}
	reader.objects[types.NamespacedName{Name: "testkind"}] = celCT

	oldStatus = status.Status.DeepCopy()
	status.Status.Errors = nil
	if _, err := r.manageVAPB(context.Background(), util.Dryrun, instance, status); err != nil {
		t.Fatalf("CEL-eligible manageVAPB returned unexpected error: %v", err)
	}
	apiError := fmt.Sprintf("cannot generate ValidatingAdmissionPolicyBinding: %s", ErrValidatingAdmissionPolicyAPIDisabled)
	if len(status.Status.Errors) != 1 || status.Status.Errors[0].Message != apiError {
		t.Fatalf("expected VAP API error %q, got %v", apiError, status.Status.Errors)
	}
	for _, ep := range status.Status.EnforcementPointsStatus {
		if ep.EnforcementPoint == util.VAPEnforcementPoint {
			t.Fatalf("expected stale missing-CEL enforcement point status to be removed, got %#v", ep)
		}
	}
	if err := r.persistPodStatus(context.Background(), status, oldStatus); err != nil {
		t.Fatalf("persisting CEL-eligible status returned unexpected error: %v", err)
	}
	if len(writer.updatedObjects) != 2 {
		t.Fatalf("expected one update for the Rego-to-CEL transition, got %d total updates", len(writer.updatedObjects))
	}

	oldStatus = status.Status.DeepCopy()
	status.Status.Errors = nil
	if _, err := r.manageVAPB(context.Background(), util.Dryrun, instance, status); err != nil {
		t.Fatalf("stable CEL-eligible manageVAPB returned unexpected error: %v", err)
	}
	if err := r.persistPodStatus(context.Background(), status, oldStatus); err != nil {
		t.Fatalf("persisting stable CEL-eligible status returned unexpected error: %v", err)
	}
	if len(writer.updatedObjects) != 2 {
		t.Fatalf("expected stable CEL-eligible status to skip another update, got %d total updates", len(writer.updatedObjects))
	}
}

func TestManageVAPB_VAPAPIDisabledStatusIsIdempotent(t *testing.T) {
	configureVAP(t, vapTestConfig{
		apiEnabled:          ptr.To(false),
		defaultGenerateVAP:  ptr.To(true),
		defaultGenerateVAPB: ptr.To(true),
	})

	scheme := runtime.NewScheme()
	if err := templatesv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	ct := &templatesv1beta1.ConstraintTemplate{}
	if err := scheme.Convert(makeTemplateWithCELEngine(nil), ct, nil); err != nil {
		t.Fatal(err)
	}

	instance := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "TestKind",
			"metadata": map[string]interface{}{
				"generation": int64(1),
				"name":       "test-constraint",
				"uid":        "12345",
			},
			"spec": map[string]interface{}{
				"enforcementAction": "dryrun",
			},
		},
	}

	errorMessage := fmt.Sprintf("cannot generate ValidatingAdmissionPolicyBinding: %s", ErrValidatingAdmissionPolicyAPIDisabled)
	status := &constraintstatusv1beta1.ConstraintPodStatus{
		Status: constraintstatusv1beta1.ConstraintPodStatusStatus{
			Errors: []constraintstatusv1beta1.Error{{Message: errorMessage}},
		},
	}
	oldStatus := status.Status.DeepCopy()
	status.Status.Errors = nil
	writer := &trackingWriter{}
	r := &ReconcileConstraint{
		reader: &fakeReader{
			objects: map[types.NamespacedName]client.Object{
				{Name: "testkind"}: ct,
			},
		},
		writer:   writer,
		log:      logf.Log.WithName("test"),
		reporter: &fakeReporter{},
		scheme:   scheme,
	}

	if _, err := r.manageVAPB(context.Background(), util.Dryrun, instance, status); err != nil {
		t.Fatalf("manageVAPB returned unexpected error: %v", err)
	}
	if len(status.Status.Errors) != 1 || status.Status.Errors[0].Message != errorMessage {
		t.Fatalf("expected stable VAP API error %q, got %v", errorMessage, status.Status.Errors)
	}
	if err := r.persistPodStatus(context.Background(), status, oldStatus); err != nil {
		t.Fatalf("persistPodStatus returned unexpected error: %v", err)
	}
	if len(writer.updatedObjects) != 0 {
		t.Fatalf("expected unchanged API-disabled status to skip update, got %d", len(writer.updatedObjects))
	}
}

func TestManageVAPB_NoStaleVAPB_NoDelete(t *testing.T) {
	// When vap.k8s.io is removed and no VAPB exists, Delete should not be called.

	configureVAP(t, vapTestConfig{
		apiEnabled:          ptr.To(true),
		defaultGenerateVAPB: ptr.To(true),
	})

	instance := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "TestKind",
			"metadata": map[string]interface{}{
				"name": "test-constraint",
				"uid":  "12345",
			},
			"spec": map[string]interface{}{
				"enforcementAction": "scoped",
				"scopedEnforcementActions": []interface{}{
					map[string]interface{}{
						"action": "deny",
						"enforcementPoints": []interface{}{
							map[string]interface{}{"name": util.WebhookEnforcementPoint},
						},
					},
				},
			},
		},
	}

	ct := &templatesv1beta1.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testkind",
		},
	}

	reader := &fakeReader{
		objects: map[types.NamespacedName]client.Object{
			{Name: "testkind"}: ct,
			// No stale VAPB — the reader will return NotFound for VAPB lookup.
		},
	}

	writer := &trackingWriter{}

	status := &constraintstatusv1beta1.ConstraintPodStatus{
		Status: constraintstatusv1beta1.ConstraintPodStatusStatus{},
	}

	reporter := &fakeReporter{
		vapbStatuses: map[types.NamespacedName]metrics.VAPStatus{
			{Name: "gatekeeper-testkind-test-constraint"}: metrics.VAPStatusError,
		},
	}

	r := &ReconcileConstraint{
		reader:   reader,
		writer:   writer,
		log:      logf.Log.WithName("test"),
		reporter: reporter,
		scheme:   runtime.NewScheme(),
	}

	_, err := r.manageVAPB(context.Background(), util.Scoped, instance, status)
	if err != nil {
		t.Fatalf("manageVAPB returned unexpected error: %v", err)
	}

	if len(writer.deletedObjects) != 0 {
		t.Fatalf("expected no VAPB deletions when no stale VAPB exists, got %d", len(writer.deletedObjects))
	}
	if len(writer.updatedObjects) != 0 {
		t.Fatalf("expected no status updates when VAPB status is unchanged, got %d", len(writer.updatedObjects))
	}

	if _, exists := reporter.vapbStatuses[types.NamespacedName{Name: "gatekeeper-testkind-test-constraint"}]; exists {
		t.Fatal("expected VAPB metric to be deleted when constraint no longer intends to use VAP")
	}
}

func BenchmarkManageVAPBNoopStatus(b *testing.B) {
	gv := schema.GroupVersion{Group: "admissionregistration.k8s.io", Version: "v1"}
	transform.SetVapAPIEnabled(ptr.To(true))
	transform.SetGroupVersion(&gv)
	b.Cleanup(func() {
		transform.SetVapAPIEnabled(nil)
		transform.SetGroupVersion(nil)
	})

	origDefault := GetDefaultGenerateVAPB()
	SetDefaultGenerateVAPB(true)
	b.Cleanup(func() { SetDefaultGenerateVAPB(origDefault) })

	instance := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "TestKind",
			"metadata": map[string]interface{}{
				"name": "test-constraint",
				"uid":  "12345",
			},
			"spec": map[string]interface{}{
				"enforcementAction": "scoped",
				"scopedEnforcementActions": []interface{}{
					map[string]interface{}{
						"action": "deny",
						"enforcementPoints": []interface{}{
							map[string]interface{}{"name": util.WebhookEnforcementPoint},
						},
					},
				},
			},
		},
	}

	ct := &templatesv1beta1.ConstraintTemplate{ObjectMeta: metav1.ObjectMeta{Name: "testkind"}}
	reader := &fakeReader{objects: map[types.NamespacedName]client.Object{{Name: "testkind"}: ct}}
	writer := &trackingWriter{}
	reporter := &fakeReporter{vapbStatuses: map[types.NamespacedName]metrics.VAPStatus{{Name: "gatekeeper-testkind-test-constraint"}: metrics.VAPStatusError}}
	status := &constraintstatusv1beta1.ConstraintPodStatus{Status: constraintstatusv1beta1.ConstraintPodStatusStatus{}}
	r := &ReconcileConstraint{reader: reader, writer: writer, log: logf.Log.WithName("test"), reporter: reporter, scheme: runtime.NewScheme()}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.manageVAPB(context.Background(), util.Scoped, instance, status); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(len(writer.updatedObjects))/float64(b.N), "updates/op")
}

func TestManageVAPB_SkipsDeleteIfNotOwner(t *testing.T) {
	// Regression test: a VAPB owned by a different constraint kind with the same name
	// must NOT be deleted by this constraint's cleanup path.

	configureVAP(t, vapTestConfig{
		apiEnabled:          ptr.To(true),
		defaultGenerateVAPB: ptr.To(true),
	})

	// This constraint does NOT use vap.k8s.io.
	instance := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "TestKindB",
			"metadata": map[string]interface{}{
				"name": "test-constraint",
				"uid":  "other-uid-67890",
			},
			"spec": map[string]interface{}{
				"enforcementAction": "scoped",
				"scopedEnforcementActions": []interface{}{
					map[string]interface{}{
						"action": "deny",
						"enforcementPoints": []interface{}{
							map[string]interface{}{"name": util.WebhookEnforcementPoint},
						},
					},
				},
			},
		},
	}

	// VAPB with same name but owned by a DIFFERENT constraint (TestKindA/test-constraint).
	vapbOwnedByOther := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gatekeeper-testkindb-test-constraint",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "constraints.gatekeeper.sh/v1beta1",
				Kind:       "TestKindA",
				Name:       "test-constraint",
				UID:        "original-uid-12345",
				Controller: ptr.To(true),
			}},
		},
	}

	ct := &templatesv1beta1.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testkindb",
		},
	}

	reader := &fakeReader{
		objects: map[types.NamespacedName]client.Object{
			{Name: "testkindb"}:                            ct,
			{Name: "gatekeeper-testkindb-test-constraint"}: vapbOwnedByOther,
		},
	}

	writer := &trackingWriter{}

	status := &constraintstatusv1beta1.ConstraintPodStatus{
		Status: constraintstatusv1beta1.ConstraintPodStatusStatus{},
	}

	r := &ReconcileConstraint{
		reader:   reader,
		writer:   writer,
		log:      logf.Log.WithName("test"),
		reporter: &fakeReporter{},
		scheme:   runtime.NewScheme(),
	}

	_, err := r.manageVAPB(context.Background(), util.Scoped, instance, status)
	if err != nil {
		t.Fatalf("manageVAPB returned unexpected error: %v", err)
	}

	if len(writer.deletedObjects) != 0 {
		t.Fatalf("expected no VAPB deletions when VAPB is owned by a different constraint, got %d", len(writer.deletedObjects))
	}
	if len(writer.updatedObjects) != 0 {
		t.Fatalf("expected no status updates when VAPB is skipped and status is unchanged, got %d", len(writer.updatedObjects))
	}
}

func TestDeleteVAPBIfOwned_FallsBackToOwnerCoordinatesWhenUIDMissing(t *testing.T) {
	instance := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "TestKind",
			"metadata": map[string]interface{}{
				"name": "test-constraint",
			},
		},
	}

	vapBinding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gatekeeper-testkind-test-constraint",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "constraints.gatekeeper.sh/v1beta1",
				Kind:       "TestKind",
				Name:       "test-constraint",
				UID:        "original-uid-12345",
				Controller: ptr.To(true),
			}},
		},
	}

	writer := &trackingWriter{}
	r := &ReconcileConstraint{
		apiReader: &fakeReader{},
		writer:    writer,
		log:       logf.Log.WithName("test"),
		reporter:  &fakeReporter{},
	}

	if err := r.deleteVAPBIfOwned(context.Background(), vapBinding, instance, vapBinding.GetName()); err != nil {
		t.Fatalf("deleteVAPBIfOwned returned unexpected error: %v", err)
	}

	if len(writer.deletedObjects) != 1 {
		t.Fatalf("expected 1 VAPB to be deleted when UID is missing but owner coordinates match, got %d", len(writer.deletedObjects))
	}
}

func TestManageVAPB_EnforcementPointStatusCleanup(t *testing.T) {
	tests := []struct {
		name              string
		useVAPEnforcement bool
		regoOnlyTemplate  bool
		initialEPState    string
		initialEPMessage  string
		expectEPCleaned   bool
		expectEPState     string
		expectEPMessage   string
		expectUpdateCount int
	}{
		{
			name:              "stale generated status cleaned when no VAPB exists",
			useVAPEnforcement: false,
			initialEPState:    GeneratedVAPBState,
			expectEPCleaned:   true,
			expectUpdateCount: 1,
		},
		{
			name:              "stale error status cleaned when vap.k8s.io removed",
			useVAPEnforcement: false,
			initialEPState:    ErrGenerateVAPBState,
			expectEPCleaned:   true,
			expectUpdateCount: 1,
		},
		{
			name:              "error status preserved for rego-only template with vap.k8s.io",
			useVAPEnforcement: true,
			regoOnlyTemplate:  true,
			initialEPState:    "",
			expectEPCleaned:   false,
			expectEPState:     ErrGenerateVAPBState,
			expectEPMessage:   celSchema.ErrCELEngineMissing.Error(),
			expectUpdateCount: 1,
		},
		{
			name:              "stale error status updated in place for rego-only template with vap.k8s.io",
			useVAPEnforcement: true,
			regoOnlyTemplate:  true,
			initialEPState:    ErrGenerateVAPBState,
			initialEPMessage:  "stale error",
			expectEPCleaned:   false,
			expectEPState:     ErrGenerateVAPBState,
			expectEPMessage:   celSchema.ErrCELEngineMissing.Error(),
			expectUpdateCount: 1,
		},
		{
			name:              "matching error status skips final update for rego-only template with vap.k8s.io",
			useVAPEnforcement: true,
			regoOnlyTemplate:  true,
			initialEPState:    ErrGenerateVAPBState,
			initialEPMessage:  celSchema.ErrCELEngineMissing.Error(),
			expectEPCleaned:   false,
			expectEPState:     ErrGenerateVAPBState,
			expectEPMessage:   celSchema.ErrCELEngineMissing.Error(),
			expectUpdateCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configureVAP(t, vapTestConfig{
				apiEnabled:          ptr.To(true),
				defaultGenerateVAPB: ptr.To(true),
			})

			epName := util.WebhookEnforcementPoint
			if tt.useVAPEnforcement {
				epName = util.VAPEnforcementPoint
			}

			instance := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "constraints.gatekeeper.sh/v1beta1",
					"kind":       "TestKind",
					"metadata": map[string]interface{}{
						"generation": int64(1),
						"name":       "test-constraint",
						"uid":        "12345",
					},
					"spec": map[string]interface{}{
						"enforcementAction": "scoped",
						"scopedEnforcementActions": []interface{}{
							map[string]interface{}{
								"action": "deny",
								"enforcementPoints": []interface{}{
									map[string]interface{}{"name": epName},
								},
							},
						},
					},
				},
			}

			s := runtime.NewScheme()
			var ct *templatesv1beta1.ConstraintTemplate

			if tt.regoOnlyTemplate {
				if err := templatesv1beta1.AddToScheme(s); err != nil {
					t.Fatal(err)
				}
				regoOnlyCT := makeTemplateWithRegoEngine()
				ct = &templatesv1beta1.ConstraintTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "testkind"},
				}
				if err := s.Convert(regoOnlyCT, ct, nil); err != nil {
					t.Fatal(err)
				}
			} else {
				ct = &templatesv1beta1.ConstraintTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "testkind"},
				}
			}

			reader := &fakeReader{
				objects: map[types.NamespacedName]client.Object{
					{Name: "testkind"}: ct,
				},
			}

			status := &constraintstatusv1beta1.ConstraintPodStatus{
				Status: constraintstatusv1beta1.ConstraintPodStatusStatus{},
			}
			if tt.initialEPState != "" {
				status.Status.EnforcementPointsStatus = []constraintstatusv1beta1.EnforcementPointStatus{
					{
						EnforcementPoint:   util.VAPEnforcementPoint,
						State:              tt.initialEPState,
						Message:            tt.initialEPMessage,
						ObservedGeneration: 1,
					},
				}
			}

			writer := &trackingWriter{}
			r := &ReconcileConstraint{
				reader:   reader,
				writer:   writer,
				log:      logf.Log.WithName("test"),
				reporter: &fakeReporter{},
				scheme:   s,
			}

			oldStatus := status.Status.DeepCopy()
			_, err := r.manageVAPB(context.Background(), util.Scoped, instance, status)
			if err != nil {
				t.Fatalf("manageVAPB returned unexpected error: %v", err)
			}
			if err := r.persistPodStatus(context.Background(), status, oldStatus); err != nil {
				t.Fatalf("persistPodStatus returned unexpected error: %v", err)
			}

			var foundEP *constraintstatusv1beta1.EnforcementPointStatus
			for i, ep := range status.Status.EnforcementPointsStatus {
				if ep.EnforcementPoint == util.VAPEnforcementPoint {
					foundEP = &status.Status.EnforcementPointsStatus[i]
					break
				}
			}

			if tt.expectEPCleaned {
				if foundEP != nil {
					t.Fatalf("expected EP status to be cleaned, but found: %+v", *foundEP)
				}
			} else {
				if foundEP == nil {
					t.Fatal("expected EP status to be preserved, but it was removed")
				}
				if foundEP.State != tt.expectEPState {
					t.Fatalf("expected EP state %q, got %q", tt.expectEPState, foundEP.State)
				}
				if tt.expectEPMessage != "" && foundEP.Message != tt.expectEPMessage {
					t.Fatalf("expected EP message %q, got %q", tt.expectEPMessage, foundEP.Message)
				}
			}
			if len(writer.updatedObjects) != tt.expectUpdateCount {
				t.Fatalf("expected %d status updates, got %d", tt.expectUpdateCount, len(writer.updatedObjects))
			}
		})
	}
}

func TestManageVAPB_WaitStatusIsIdempotent(t *testing.T) {
	configureVAP(t, vapTestConfig{
		apiEnabled:          ptr.To(true),
		defaultGenerateVAP:  ptr.To(true),
		defaultGenerateVAPB: ptr.To(true),
	})

	scheme := runtime.NewScheme()
	if err := templatesv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	unversionedCT := makeTemplateWithCELEngine(nil)
	ct := &templatesv1beta1.ConstraintTemplate{}
	if err := scheme.Convert(unversionedCT, ct, nil); err != nil {
		t.Fatal(err)
	}
	unblockAt := time.Now().Add(30 * time.Second).Format(time.RFC3339)
	ct.Annotations = map[string]string{
		BlockVAPBGenerationUntilAnnotation: unblockAt,
		VAPBGenerationAnnotation:           VAPBGenerationBlocked,
	}

	instance := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "TestKind",
			"metadata": map[string]interface{}{
				"generation": int64(1),
				"name":       "test-constraint",
				"uid":        "12345",
			},
			"spec": map[string]interface{}{
				"enforcementAction": "scoped",
				"scopedEnforcementActions": []interface{}{
					map[string]interface{}{
						"action": "deny",
						"enforcementPoints": []interface{}{
							map[string]interface{}{"name": util.VAPEnforcementPoint},
						},
					},
				},
			},
		},
	}

	reader := &fakeReader{
		objects: map[types.NamespacedName]client.Object{
			{Name: "testkind"}: ct,
		},
	}
	writer := &trackingWriter{}
	status := &constraintstatusv1beta1.ConstraintPodStatus{}
	r := &ReconcileConstraint{
		reader:   reader,
		writer:   writer,
		log:      logf.Log.WithName("test"),
		reporter: &fakeReporter{},
		scheme:   scheme,
	}

	requeueAfter, err := r.manageVAPB(context.Background(), util.Scoped, instance, status)
	if err != nil {
		t.Fatalf("manageVAPB returned unexpected error: %v", err)
	}
	if requeueAfter <= 0 {
		t.Fatalf("expected positive requeue delay while VAPB generation is blocked, got %s", requeueAfter)
	}
	if len(writer.updatedObjects) != 0 {
		t.Fatalf("expected manageVAPB to leave status persistence to Reconcile, got %d updates", len(writer.updatedObjects))
	}
	if len(status.Status.EnforcementPointsStatus) != 1 {
		t.Fatalf("expected one enforcement point status, got %d", len(status.Status.EnforcementPointsStatus))
	}
	firstEPStatus := status.Status.EnforcementPointsStatus[0]
	if firstEPStatus.State != WaitVAPBState {
		t.Fatalf("expected EP state %q, got %q", WaitVAPBState, firstEPStatus.State)
	}
	if want := fmt.Sprintf("waiting until %s before generating ValidatingAdmissionPolicyBinding to make sure api-server has cached constraint CRD", unblockAt); firstEPStatus.Message != want {
		t.Fatalf("expected stable wait message %q, got %q", want, firstEPStatus.Message)
	}

	beforeSecondReconcile := status.DeepCopy()
	requeueAfter, err = r.manageVAPB(context.Background(), util.Scoped, instance, status)
	if err != nil {
		t.Fatalf("second manageVAPB returned unexpected error: %v", err)
	}
	if requeueAfter <= 0 {
		t.Fatalf("expected positive requeue delay on second wait reconcile, got %s", requeueAfter)
	}
	if len(writer.updatedObjects) != 0 {
		t.Fatalf("expected manageVAPB to leave status persistence to Reconcile, got %d total updates", len(writer.updatedObjects))
	}
	if !reflect.DeepEqual(beforeSecondReconcile.Status, status.Status) {
		t.Fatalf("expected second wait reconcile to leave status unchanged; diff: %s", spew.Sdump(status.Status))
	}
}

func TestManageVAPB_PreservesErrorMetricWhenGenerationFails(t *testing.T) {
	configureVAP(t, vapTestConfig{
		apiEnabled:          ptr.To(true),
		defaultGenerateVAPB: ptr.To(true),
	})

	instance := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "TestKind",
			"metadata": map[string]interface{}{
				"name": "test-constraint",
				"uid":  "12345",
			},
			"spec": map[string]interface{}{
				"enforcementAction": "scoped",
				"scopedEnforcementActions": []interface{}{
					map[string]interface{}{
						"action": "deny",
						"enforcementPoints": []interface{}{
							map[string]interface{}{"name": util.VAPEnforcementPoint},
						},
					},
				},
			},
		},
	}

	s := runtime.NewScheme()
	if err := templatesv1beta1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	regoOnlyCT := makeTemplateWithRegoEngine()
	ct := &templatesv1beta1.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "testkind"},
	}
	if err := s.Convert(regoOnlyCT, ct, nil); err != nil {
		t.Fatal(err)
	}

	staleVAPB := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gatekeeper-testkind-test-constraint",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "constraints.gatekeeper.sh/v1beta1",
				Kind:       "TestKind",
				Name:       "test-constraint",
				UID:        "12345",
				Controller: ptr.To(true),
			}},
		},
	}

	reader := &fakeReader{
		objects: map[types.NamespacedName]client.Object{
			{Name: "testkind"}: ct,
			{Name: "gatekeeper-testkind-test-constraint"}: staleVAPB,
		},
	}

	writer := &trackingWriter{}
	reporter := &fakeReporter{}
	status := &constraintstatusv1beta1.ConstraintPodStatus{
		Status: constraintstatusv1beta1.ConstraintPodStatusStatus{},
	}

	r := &ReconcileConstraint{
		reader:   reader,
		writer:   writer,
		log:      logf.Log.WithName("test"),
		reporter: reporter,
		scheme:   s,
	}

	_, err := r.manageVAPB(context.Background(), util.Scoped, instance, status)
	if err != nil {
		t.Fatalf("manageVAPB returned unexpected error: %v", err)
	}

	if len(writer.deletedObjects) != 1 {
		t.Fatalf("expected stale VAPB to be deleted, got %d deletions", len(writer.deletedObjects))
	}

	vapBindingKey := types.NamespacedName{Name: "gatekeeper-testkind-test-constraint"}
	if got := reporter.vapbStatuses[vapBindingKey]; got != metrics.VAPStatusError {
		t.Fatalf("expected VAPB metric status %q after generation failure, got %q", metrics.VAPStatusError, got)
	}
}

type fakeWriter struct {
	updateErr error
	createErr error
}

func (f *fakeWriter) Update(_ context.Context, _ client.Object, _ ...client.UpdateOption) error {
	return f.updateErr
}

func (f *fakeWriter) Create(_ context.Context, _ client.Object, _ ...client.CreateOption) error {
	return f.createErr
}

func (f *fakeWriter) Delete(_ context.Context, _ client.Object, _ ...client.DeleteOption) error {
	return nil
}

func (f *fakeWriter) Patch(_ context.Context, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
	return nil
}

func (f *fakeWriter) DeleteAllOf(_ context.Context, _ client.Object, _ ...client.DeleteAllOfOption) error {
	return nil
}

func (f *fakeWriter) Apply(_ context.Context, _ runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
	return nil
}

func TestEventPackerMapFuncFromOwnerRefs_SingleOwner(t *testing.T) {
	mf := eventPackerMapFuncFromOwnerRefs()
	obj := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
	// cluster-scoped object
	obj.SetName("vap-binding-test")
	obj.SetNamespace("")
	// set owner reference to a constraint kind in constraints.gatekeeper.sh group
	obj.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: "constraints.gatekeeper.sh/v1beta1",
		Kind:       "MyConstraint",
		Name:       "my-constraint-name",
		Controller: func(b bool) *bool { return &b }(true),
	}})

	reqs := mf(context.Background(), obj)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	gvk, unpacked, err := util.UnpackRequest(reqs[0])
	if err != nil {
		t.Fatalf("unpack request failed: %v", err)
	}
	if gvk.Group != "constraints.gatekeeper.sh" {
		t.Fatalf("unexpected group: %s", gvk.Group)
	}
	if gvk.Version != "v1beta1" {
		t.Fatalf("unexpected version: %s", gvk.Version)
	}
	if gvk.Kind != "MyConstraint" {
		t.Fatalf("unexpected kind: %s", gvk.Kind)
	}
	if unpacked.Name != "my-constraint-name" {
		t.Fatalf("unexpected name: %s", unpacked.Name)
	}
}

func TestEventPackerMapFuncFromOwnerRefs_MultipleOwners(t *testing.T) {
	mf := eventPackerMapFuncFromOwnerRefs()
	obj := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
	obj.SetName("vap-binding-multi")
	obj.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: "other.group/v1",
			Kind:       "OtherKind",
			Name:       "other-name",
			Controller: func(b bool) *bool { return &b }(true),
		},
		{
			APIVersion: "constraints.gatekeeper.sh/v1beta1",
			Kind:       "FooConstraint",
			Name:       "foo-name",
			Controller: func(b bool) *bool { return &b }(true),
		},
	})

	reqs := mf(context.Background(), obj)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request for the matching owner, got %d", len(reqs))
	}
	gvk, unpacked, err := util.UnpackRequest(reqs[0])
	if err != nil {
		t.Fatalf("unpack request failed: %v", err)
	}
	if gvk.Kind != "FooConstraint" || gvk.Group != "constraints.gatekeeper.sh" {
		t.Fatalf("unexpected gvk: %v", gvk)
	}
	if unpacked.Name != "foo-name" {
		t.Fatalf("unexpected name: %s", unpacked.Name)
	}
}

func TestCleanupLegacyVAPB(t *testing.T) {
	// When a new-format VAPB has been created, the old-format (legacy) VAPB
	// owned by the same constraint must be cleaned up.

	gv := admissionregistrationv1.SchemeGroupVersion

	instance := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "TestKind",
			"metadata": map[string]interface{}{
				"name": "test-constraint",
				"uid":  "12345",
			},
		},
	}

	// Legacy VAPB (old format without Kind) owned by this constraint.
	legacyVAPB := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gatekeeper-test-constraint",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "constraints.gatekeeper.sh/v1beta1",
				Kind:       "TestKind",
				Name:       "test-constraint",
				UID:        "12345",
				Controller: ptr.To(true),
			}},
		},
	}

	reader := &fakeReader{
		objects: map[types.NamespacedName]client.Object{
			{Name: "gatekeeper-test-constraint"}: legacyVAPB,
		},
	}

	writer := &trackingWriter{}

	r := &ReconcileConstraint{
		reader:   reader,
		writer:   writer,
		log:      logf.Log.WithName("test"),
		reporter: &fakeReporter{},
		scheme:   runtime.NewScheme(),
	}

	if err := r.cleanupLegacyVAPB(context.Background(), instance, &gv); err != nil {
		t.Fatalf("cleanupLegacyVAPB returned unexpected error: %v", err)
	}

	if len(writer.deletedObjects) != 1 {
		t.Fatalf("expected 1 legacy VAPB to be deleted, got %d", len(writer.deletedObjects))
	}

	deletedVAPB, ok := writer.deletedObjects[0].(*admissionregistrationv1.ValidatingAdmissionPolicyBinding)
	if !ok {
		t.Fatalf("deleted object is not a ValidatingAdmissionPolicyBinding, got %T", writer.deletedObjects[0])
	}
	if deletedVAPB.Name != "gatekeeper-test-constraint" {
		t.Errorf("expected deleted VAPB name 'gatekeeper-test-constraint', got %q", deletedVAPB.Name)
	}
}

func TestCleanupLegacyVAPB_SkipsIfNotOwner(t *testing.T) {
	// Legacy VAPB owned by a different constraint must NOT be deleted.

	gv := admissionregistrationv1.SchemeGroupVersion

	instance := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "TestKindB",
			"metadata": map[string]interface{}{
				"name": "test-constraint",
				"uid":  "other-uid-67890",
			},
		},
	}

	// Legacy VAPB owned by a DIFFERENT constraint kind.
	legacyVAPB := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gatekeeper-test-constraint",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "constraints.gatekeeper.sh/v1beta1",
				Kind:       "TestKindA",
				Name:       "test-constraint",
				UID:        "original-uid-12345",
				Controller: ptr.To(true),
			}},
		},
	}

	reader := &fakeReader{
		objects: map[types.NamespacedName]client.Object{
			{Name: "gatekeeper-test-constraint"}: legacyVAPB,
		},
	}

	writer := &trackingWriter{}

	r := &ReconcileConstraint{
		reader:   reader,
		writer:   writer,
		log:      logf.Log.WithName("test"),
		reporter: &fakeReporter{},
		scheme:   runtime.NewScheme(),
	}

	if err := r.cleanupLegacyVAPB(context.Background(), instance, &gv); err != nil {
		t.Fatalf("cleanupLegacyVAPB returned unexpected error: %v", err)
	}

	if len(writer.deletedObjects) != 0 {
		t.Fatalf("expected no legacy VAPB deletions when owned by different constraint, got %d", len(writer.deletedObjects))
	}
}

func TestCleanupLegacyVAPB_FallsBackToOwnerCoordinatesWhenUIDMissing(t *testing.T) {
	gv := admissionregistrationv1.SchemeGroupVersion

	instance := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "TestKind",
			"metadata": map[string]interface{}{
				"name": "test-constraint",
			},
		},
	}

	legacyVAPB := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gatekeeper-test-constraint",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "constraints.gatekeeper.sh/v1beta1",
				Kind:       "TestKind",
				Name:       "test-constraint",
				UID:        "original-uid-12345",
				Controller: ptr.To(true),
			}},
		},
	}

	reader := &fakeReader{
		objects: map[types.NamespacedName]client.Object{
			{Name: "gatekeeper-test-constraint"}: legacyVAPB,
		},
	}

	writer := &trackingWriter{}
	r := &ReconcileConstraint{
		reader:    reader,
		apiReader: reader,
		writer:    writer,
		log:       logf.Log.WithName("test"),
		reporter:  &fakeReporter{},
		scheme:    runtime.NewScheme(),
	}

	if err := r.cleanupLegacyVAPB(context.Background(), instance, &gv); err != nil {
		t.Fatalf("cleanupLegacyVAPB returned unexpected error: %v", err)
	}

	if len(writer.deletedObjects) != 1 {
		t.Fatalf("expected 1 legacy VAPB to be deleted when UID is missing but owner coordinates match, got %d", len(writer.deletedObjects))
	}
}

func TestCleanupLegacyVAPB_SkipsFallbackWhenOwnerCoordinatesDoNotMatch(t *testing.T) {
	gv := admissionregistrationv1.SchemeGroupVersion

	instance := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "constraints.gatekeeper.sh/v1beta1",
			"kind":       "TestKind",
			"metadata": map[string]interface{}{
				"name": "test-constraint",
			},
		},
	}

	legacyVAPB := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gatekeeper-test-constraint",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "constraints.gatekeeper.sh/v1beta1",
				Kind:       "OtherKind",
				Name:       "test-constraint",
				UID:        "original-uid-12345",
				Controller: ptr.To(true),
			}},
		},
	}

	reader := &fakeReader{
		objects: map[types.NamespacedName]client.Object{
			{Name: "gatekeeper-test-constraint"}: legacyVAPB,
		},
	}

	writer := &trackingWriter{}
	r := &ReconcileConstraint{
		reader:   reader,
		writer:   writer,
		log:      logf.Log.WithName("test"),
		reporter: &fakeReporter{},
		scheme:   runtime.NewScheme(),
	}

	if err := r.cleanupLegacyVAPB(context.Background(), instance, &gv); err != nil {
		t.Fatalf("cleanupLegacyVAPB returned unexpected error: %v", err)
	}

	if len(writer.deletedObjects) != 0 {
		t.Fatalf("expected no legacy VAPB deletions when UID is missing and owner coordinates do not match, got %d", len(writer.deletedObjects))
	}
}

func TestGetVAPBindingName(t *testing.T) {
	// New format includes Kind.
	name := transform.GetVAPBindingName("K8sRequiredLabels", "my-policy")
	expected := "gatekeeper-k8srequiredlabels-my-policy"
	if name != expected {
		t.Errorf("expected %q, got %q", expected, name)
	}
}

func TestGetVAPBindingName_Truncation(t *testing.T) {
	// Kind and name that together exceed the 253-char K8s limit.
	longKind := strings.Repeat("a", 63)
	longName := strings.Repeat("b", 253)
	name := transform.GetVAPBindingName(longKind, longName)
	if len(name) > 253 {
		t.Errorf("expected name length <= 253, got %d: %s", len(name), name)
	}
	// Verify deterministic: same input produces same output.
	name2 := transform.GetVAPBindingName(longKind, longName)
	if name != name2 {
		t.Errorf("expected deterministic name, got %q and %q", name, name2)
	}
	// Short names should not be truncated.
	shortName := transform.GetVAPBindingName("Kind", "my-constraint")
	if strings.Contains(shortName, "-") && len(shortName) <= 253 {
		expectedShort := "gatekeeper-kind-my-constraint"
		if shortName != expectedShort {
			t.Errorf("expected %q, got %q", expectedShort, shortName)
		}
	}
}

func TestLegacyVAPBindingName(t *testing.T) {
	name := transform.LegacyVAPBindingName("my-policy")
	expected := "gatekeeper-my-policy"
	if name != expected {
		t.Errorf("expected %q, got %q", expected, name)
	}
}
