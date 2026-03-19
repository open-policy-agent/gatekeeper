package constraint

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
	apiconstraints "github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	templatesv1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1"
	templatesv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	regoSchema "github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego/schema"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	constraintstatusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	celSchema "github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/transform"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
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
			DefaultGenerateVAP = ptr.To[bool](test.vapDefault)
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
	tests := []struct {
		name                   string
		status                 *constraintstatusv1beta1.ConstraintPodStatus
		err                    error
		message                string
		updateErr              error
		expectedError          error
		expectedStatusErrorLen int
		expectedStatusError    []constraintstatusv1beta1.Error
	}{
		{
			name: "successful update",
			status: &constraintstatusv1beta1.ConstraintPodStatus{
				Status: constraintstatusv1beta1.ConstraintPodStatusStatus{},
			},
			err:                    errors.New("test error"),
			message:                "test message",
			updateErr:              nil,
			expectedError:          errors.New("test error"),
			expectedStatusErrorLen: 1,
			expectedStatusError: []constraintstatusv1beta1.Error{
				{
					Message: "test error",
				},
			},
		},
		{
			name: "update error",
			status: &constraintstatusv1beta1.ConstraintPodStatus{
				Status: constraintstatusv1beta1.ConstraintPodStatusStatus{},
			},
			err:                    errors.New("test error"),
			message:                "test message",
			updateErr:              errors.New("update error"),
			expectedError:          errors.New("test message, could not update constraint status: update error: test error"),
			expectedStatusErrorLen: 1,
			expectedStatusError: []constraintstatusv1beta1.Error{
				{
					Message: "test error",
				},
			},
		},
		{
			name: "append status error",
			status: &constraintstatusv1beta1.ConstraintPodStatus{
				Status: constraintstatusv1beta1.ConstraintPodStatusStatus{
					Errors: []constraintstatusv1beta1.Error{
						{
							Message: "existing error",
						},
					},
				},
			},
			err:                    errors.New("test error"),
			message:                "test message",
			updateErr:              nil,
			expectedError:          errors.New("test error"),
			expectedStatusErrorLen: 2,
			expectedStatusError: []constraintstatusv1beta1.Error{
				{
					Message: "existing error",
				},
				{
					Message: "test error",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &fakeWriter{updateErr: tt.updateErr}
			r := &ReconcileConstraint{
				writer: writer,
			}

			err := r.reportErrorOnConstraintStatus(context.TODO(), tt.status, tt.err, tt.message)
			if err == nil || err.Error() != tt.expectedError.Error() {
				t.Errorf("expected error %v, got %v", tt.expectedError, err)
			}

			if len(tt.status.Status.Errors) != tt.expectedStatusErrorLen {
				t.Errorf("expected %d error in status, got %d", tt.expectedStatusErrorLen, len(tt.status.Status.Errors))
			}

			if reflect.DeepEqual(tt.status.Status.Errors, tt.expectedStatusError) {
				t.Errorf("expected status errors %v, got %v", tt.expectedStatusError, tt.status.Status.Errors)
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
	// Expect packed name format: gvk:Kind.Version.Group:Name
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
}

func (f *fakeReader) Get(_ context.Context, key types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
	if f.getErr != nil {
		return f.getErr
	}
	stored, ok := f.objects[key]
	if !ok {
		return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
	}
	// Copy stored object data into the output parameter.
	switch dst := obj.(type) {
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
	deletedObjects []client.Object
	createdObjects []client.Object
}

func (t *trackingWriter) Delete(_ context.Context, obj client.Object, _ ...client.DeleteOption) error {
	t.deletedObjects = append(t.deletedObjects, obj)
	return nil
}

func (t *trackingWriter) Create(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
	t.createdObjects = append(t.createdObjects, obj)
	return nil
}

// fakeReporter implements StatsReporter for testing.
type fakeReporter struct{}

func (f *fakeReporter) reportConstraints(_ context.Context, _ tags, _ int64) error   { return nil }
func (f *fakeReporter) ReportVAPBStatus(_ types.NamespacedName, _ metrics.VAPStatus) {}
func (f *fakeReporter) DeleteVAPBStatus(_ types.NamespacedName)                      {}

func TestManageVAPB_CleansUpStaleVAPB(t *testing.T) {
	// Regression test for https://github.com/open-policy-agent/gatekeeper/issues/4441
	// When vap.k8s.io is removed from scopedEnforcementActions, the stale VAPB must be deleted.

	// Set up the VAP API as enabled with v1 group version.
	gv := schema.GroupVersion{Group: "admissionregistration.k8s.io", Version: "v1"}
	origEnabled := transform.VapAPIEnabled
	origGV := transform.GroupVersion
	transform.SetVapAPIEnabled(ptr.To(true))
	transform.SetGroupVersion(&gv)
	t.Cleanup(func() {
		transform.SetVapAPIEnabled(origEnabled)
		transform.SetGroupVersion(origGV)
	})

	// Ensure DefaultGenerateVAPB is true (default).
	origDefault := *DefaultGenerateVAPB
	*DefaultGenerateVAPB = true
	t.Cleanup(func() { *DefaultGenerateVAPB = origDefault })

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
			{Name: "testkind"}:                        ct,
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

func TestManageVAPB_NoStaleVAPB_NoDelete(t *testing.T) {
	// When vap.k8s.io is removed and no VAPB exists, Delete should not be called.

	gv := schema.GroupVersion{Group: "admissionregistration.k8s.io", Version: "v1"}
	origEnabled := transform.VapAPIEnabled
	origGV := transform.GroupVersion
	transform.SetVapAPIEnabled(ptr.To(true))
	transform.SetGroupVersion(&gv)
	t.Cleanup(func() {
		transform.SetVapAPIEnabled(origEnabled)
		transform.SetGroupVersion(origGV)
	})

	origDefault := *DefaultGenerateVAPB
	*DefaultGenerateVAPB = true
	t.Cleanup(func() { *DefaultGenerateVAPB = origDefault })

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
		t.Fatalf("expected no VAPB deletions when no stale VAPB exists, got %d", len(writer.deletedObjects))
	}
}

func TestManageVAPB_SkipsDeleteIfNotOwner(t *testing.T) {
	// Regression test: a VAPB owned by a different constraint kind with the same name
	// must NOT be deleted by this constraint's cleanup path.

	gv := schema.GroupVersion{Group: "admissionregistration.k8s.io", Version: "v1"}
	origEnabled := transform.VapAPIEnabled
	origGV := transform.GroupVersion
	transform.SetVapAPIEnabled(ptr.To(true))
	transform.SetGroupVersion(&gv)
	t.Cleanup(func() {
		transform.SetVapAPIEnabled(origEnabled)
		transform.SetGroupVersion(origGV)
	})

	origDefault := *DefaultGenerateVAPB
	*DefaultGenerateVAPB = true
	t.Cleanup(func() { *DefaultGenerateVAPB = origDefault })

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
			{Name: "testkindb"}:                         ct,
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

type fakeWriter struct {
	updateErr error
}

func (f *fakeWriter) Update(_ context.Context, _ client.Object, _ ...client.UpdateOption) error {
	return f.updateErr
}

func (f *fakeWriter) Create(_ context.Context, _ client.Object, _ ...client.CreateOption) error {
	return nil
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

	gv := schema.GroupVersion{Group: "admissionregistration.k8s.io", Version: "v1"}
	origEnabled := transform.VapAPIEnabled
	origGV := transform.GroupVersion
	transform.SetVapAPIEnabled(ptr.To(true))
	transform.SetGroupVersion(&gv)
	t.Cleanup(func() {
		transform.SetVapAPIEnabled(origEnabled)
		transform.SetGroupVersion(origGV)
	})

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

	r.cleanupLegacyVAPB(context.Background(), instance, &gv)

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

	gv := schema.GroupVersion{Group: "admissionregistration.k8s.io", Version: "v1"}
	origEnabled := transform.VapAPIEnabled
	origGV := transform.GroupVersion
	transform.SetVapAPIEnabled(ptr.To(true))
	transform.SetGroupVersion(&gv)
	t.Cleanup(func() {
		transform.SetVapAPIEnabled(origEnabled)
		transform.SetGroupVersion(origGV)
	})

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

	r.cleanupLegacyVAPB(context.Background(), instance, &gv)

	if len(writer.deletedObjects) != 0 {
		t.Fatalf("expected no legacy VAPB deletions when owned by different constraint, got %d", len(writer.deletedObjects))
	}
}

func TestGetVAPBindingName(t *testing.T) {
	// New format includes Kind.
	name := getVAPBindingName("K8sRequiredLabels", "my-policy")
	expected := "gatekeeper-k8srequiredlabels-my-policy"
	if name != expected {
		t.Errorf("expected %q, got %q", expected, name)
	}
}

func TestLegacyVAPBindingName(t *testing.T) {
	name := legacyVAPBindingName("my-policy")
	expected := "gatekeeper-my-policy"
	if name != expected {
		t.Errorf("expected %q, got %q", expected, name)
	}
}
