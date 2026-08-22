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
	apiconstraints "github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	templatesv1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1"
	templatesv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	regodriver "github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	regoSchema "github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego/schema"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	constraintstatusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel"
	celSchema "github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/transform"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
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
	case *admissionregistrationv1.ValidatingAdmissionPolicyBinding:
		src, ok := stored.(*admissionregistrationv1.ValidatingAdmissionPolicyBinding)
		if !ok {
			return fmt.Errorf("type mismatch: expected *admissionregistrationv1.ValidatingAdmissionPolicyBinding, got %T", stored)
		}
		*dst = *src
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
	vapbStatuses map[types.NamespacedName]metrics.VAPStatus
}

func (f *fakeReporter) reportConstraints(_ context.Context, _ tags, _ int64) error { return nil }

func (f *fakeReporter) ReportVAPBStatus(name types.NamespacedName, status metrics.VAPStatus) {
	if f.vapbStatuses == nil {
		f.vapbStatuses = make(map[types.NamespacedName]metrics.VAPStatus)
	}
	f.vapbStatuses[name] = status
}

func (f *fakeReporter) DeleteVAPBStatus(name types.NamespacedName) {
	delete(f.vapbStatuses, name)
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

	if _, exists := reporter.vapbStatuses[types.NamespacedName{Name: "gatekeeper-testkind-test-constraint"}]; exists {
		t.Fatal("expected VAPB metric to be deleted when constraint no longer intends to use VAP")
	}
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
		writer:   writer,
		log:      logf.Log.WithName("test"),
		reporter: &fakeReporter{},
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
		expectEPCleaned   bool
		expectEPState     string
	}{
		{
			name:              "stale generated status cleaned when no VAPB exists",
			useVAPEnforcement: false,
			initialEPState:    GeneratedVAPBState,
			expectEPCleaned:   true,
		},
		{
			name:              "stale error status cleaned when vap.k8s.io removed",
			useVAPEnforcement: false,
			initialEPState:    ErrGenerateVAPBState,
			expectEPCleaned:   true,
		},
		{
			name:              "error status preserved for rego-only template with vap.k8s.io",
			useVAPEnforcement: true,
			regoOnlyTemplate:  true,
			initialEPState:    "",
			expectEPCleaned:   false,
			expectEPState:     ErrGenerateVAPBState,
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
						"name": "test-constraint",
						"uid":  "12345",
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
						ObservedGeneration: 1,
					},
				}
			}

			r := &ReconcileConstraint{
				reader:   reader,
				writer:   &trackingWriter{},
				log:      logf.Log.WithName("test"),
				reporter: &fakeReporter{},
				scheme:   s,
			}

			_, err := r.manageVAPB(context.Background(), util.Scoped, instance, status)
			if err != nil {
				t.Fatalf("manageVAPB returned unexpected error: %v", err)
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
