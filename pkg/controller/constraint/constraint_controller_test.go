package constraint

import (
	"errors"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	apiconstraints "github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

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

func TestHasVAPCel(t *testing.T) {
	ct := &v1beta1.ConstraintTemplate{}

	// Test when the code is empty
	ct.Spec.Targets = []v1beta1.Target{
		{
			Code: []v1beta1.Code{},
		},
	}
	expected := false
	if result := HasVAPCel(ct); result != expected {
		t.Errorf("hasVAPCel() = %v, expected %v", result, expected)
	}

	// Test when the code has only one Rego engine
	ct.Spec.Targets = []v1beta1.Target{
		{
			Code: []v1beta1.Code{
				{
					Engine: "Rego",
				},
			},
		},
	}
	expected = false
	if result := HasVAPCel(ct); result != expected {
		t.Errorf("hasVAPCel() = %v, expected %v", result, expected)
	}

	// Test when the code has multiple engines including Rego
	ct.Spec.Targets = []v1beta1.Target{
		{
			Code: []v1beta1.Code{
				{
					Engine: "Rego",
				},
				{
					Engine: "K8sNativeValidation",
				},
			},
		},
	}
	expected = true
	if result := HasVAPCel(ct); result != expected {
		t.Errorf("hasVAPCel() = %v, expected %v", result, expected)
	}

	// Test when the code has only K8sNativeValidation engine
	ct.Spec.Targets = []v1beta1.Target{
		{
			Code: []v1beta1.Code{
				{
					Engine: "K8sNativeValidation",
				},
			},
		},
	}
	expected = true
	if result := HasVAPCel(ct); result != expected {
		t.Errorf("hasVAPCel() = %v, expected %v", result, expected)
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
