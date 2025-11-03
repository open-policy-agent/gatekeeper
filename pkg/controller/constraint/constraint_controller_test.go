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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
