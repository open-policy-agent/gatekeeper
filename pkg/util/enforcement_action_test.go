package util

import (
	"errors"
	"reflect"
	"testing"

	apiconstraints "github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestValidateEnforcementAction(t *testing.T) {
	testCases := []struct {
		name       string
		action     EnforcementAction
		wantErr    error
		constraint map[string]interface{}
	}{
		{
			name:       "empty string",
			action:     "",
			wantErr:    ErrEnforcementAction,
			constraint: nil,
		},
		{
			action:     "notsupported",
			wantErr:    ErrEnforcementAction,
			constraint: nil,
		},
		{
			action:     Dryrun,
			constraint: nil,
		},
		{
			name:    "invalid spec.scopedEnforcementAction",
			action:  Scoped,
			wantErr: ErrEnforcementAction,
			constraint: map[string]interface{}{
				"spec": map[string]interface{}{
					"scopedEnforcementActions": []apiconstraints.ScopedEnforcementAction{
						{
							Action: string(Deny),
							EnforcementPoints: []apiconstraints.EnforcementPoint{
								{
									Name: AuditEnforcementPoint,
								},
							},
						},
						{
							Action: "test",
							EnforcementPoints: []apiconstraints.EnforcementPoint{
								{
									Name: AuditEnforcementPoint,
								},
							},
						},
					},
				},
			},
		},
		{
			action: Scoped,
			constraint: map[string]interface{}{
				"spec": map[string]interface{}{
					"scopedEnforcementActions": []apiconstraints.ScopedEnforcementAction{
						{
							Action: string(Deny),
							EnforcementPoints: []apiconstraints.EnforcementPoint{
								{
									Name: AuditEnforcementPoint,
								},
							},
						},
					},
				},
			},
		},
		{
			name:   "Empty enforcement point",
			action: Scoped,
			constraint: map[string]interface{}{
				"spec": map[string]interface{}{
					"scopedEnforcementActions": []apiconstraints.ScopedEnforcementAction{
						{
							Action: string(Deny),
							EnforcementPoints: []apiconstraints.EnforcementPoint{
								{
									Name: "",
								},
							},
						},
					},
				},
			},
			wantErr: ErrUnrecognizedEnforcementPoint,
		},
	}

	for _, tc := range testCases {
		if tc.name == "" {
			tc.name = string(tc.action)
		}
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateEnforcementAction(tc.action, tc.constraint)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got ValidateEnforcementAction(%q) == %v, want %v",
					tc.action, err, tc.wantErr)
			}
		})
	}
}

func TestGetEnforcementAction(t *testing.T) {
	testCases := []struct {
		name    string
		item    map[string]interface{}
		want    EnforcementAction
		wantErr error
	}{
		{
			name: "empty item",
			item: map[string]interface{}{},
			want: Deny,
		},
		{
			name: "invalid spec.enforcementAction",
			item: map[string]interface{}{
				"spec": []string{},
			},
			wantErr: ErrInvalidSpecEnforcementAction,
		},
		{
			name: "unsupported spec.enforcementAction",
			item: map[string]interface{}{
				"spec": map[string]interface{}{
					"enforcementAction": "notsupported",
				},
			},
			want: Unrecognized,
		},
		{
			name: "valid spec.enforcementAction",
			item: map[string]interface{}{
				"spec": map[string]interface{}{
					"enforcementAction": string(Dryrun),
				},
			},
			want: Dryrun,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GetEnforcementAction(tc.item)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got GetEnforcementAction() error = %v, want %v",
					err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("got GetEnforcementAction() = %v, want %v",
					got, tc.want)
			}
		})
	}
}

func TestGetScopedEnforcementAction(t *testing.T) {
	testCases := []struct {
		name          string
		item          map[string]interface{}
		expectedError error
		expectedObj   *[]apiconstraints.ScopedEnforcementAction
	}{
		{
			name: "valid scopedEnforcementActions",
			item: map[string]interface{}{
				"spec": map[string]interface{}{
					"scopedEnforcementActions": []apiconstraints.ScopedEnforcementAction{
						{
							Action: string(Deny),
							EnforcementPoints: []apiconstraints.EnforcementPoint{
								{
									Name: AuditEnforcementPoint,
								},
							},
						},
					},
				},
			},
			expectedError: nil,
			expectedObj: &[]apiconstraints.ScopedEnforcementAction{
				{
					Action: string(Deny),
					EnforcementPoints: []apiconstraints.EnforcementPoint{
						{
							Name: AuditEnforcementPoint,
						},
					},
				},
			},
		},
		{
			name: "missing scopedEnforcementActions",
			item: map[string]interface{}{
				"spec": map[string]interface{}{},
			},
			expectedError: errors.New("scopedEnforcementActions is required"),
			expectedObj:   nil,
		},
		{
			name: "invalid scopedEnforcementActions",
			item: map[string]interface{}{
				"spec": map[string]interface{}{
					"scopedEnforcementActions": "invalid",
				},
			},
			expectedError: errors.New("Could not convert JSON to scopedEnforcementActions: json: cannot unmarshal string into Go value of type []constraints.ScopedEnforcementAction"),
			expectedObj:   nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			obj, err := GetScopedEnforcementAction(tc.item)
			if err != nil && tc.expectedError != nil && !errors.Is(err, tc.expectedError) && (err.Error() != tc.expectedError.Error()) {
				t.Errorf("got GetScopedEnforcementAction() error = %v, want %v", err, tc.expectedError)
			}
			if !reflect.DeepEqual(obj, tc.expectedObj) {
				t.Errorf("got GetScopedEnforcementAction() = %v, want %v", obj, tc.expectedObj)
			}
		})
	}
}

func TestScopedActionForEP(t *testing.T) {
	testCases := []struct {
		name             string
		enforcementPoint string
		item             map[string]interface{}
		expectedActions  []string
		expectedError    error
	}{
		{
			name:             "valid enforcement point",
			enforcementPoint: AuditEnforcementPoint,
			item: map[string]interface{}{
				"spec": map[string]interface{}{
					"scopedEnforcementActions": []apiconstraints.ScopedEnforcementAction{
						{
							Action: string(Deny),
							EnforcementPoints: []apiconstraints.EnforcementPoint{
								{
									Name: AuditEnforcementPoint,
								},
							},
						},
						{
							Action: string(Warn),
							EnforcementPoints: []apiconstraints.EnforcementPoint{
								{
									Name: WebhookEnforcementPoint,
								},
							},
						},
					},
				},
			},
			expectedActions: []string{string(Deny)},
			expectedError:   nil,
		},
		{
			name:             "multiple enforcement points",
			enforcementPoint: WebhookEnforcementPoint,
			item: map[string]interface{}{
				"spec": map[string]interface{}{
					"scopedEnforcementActions": []apiconstraints.ScopedEnforcementAction{
						{
							Action: string(Deny),
							EnforcementPoints: []apiconstraints.EnforcementPoint{
								{
									Name: AuditEnforcementPoint,
								},
								{
									Name: WebhookEnforcementPoint,
								},
							},
						},
						{
							Action: string(Warn),
							EnforcementPoints: []apiconstraints.EnforcementPoint{
								{
									Name: WebhookEnforcementPoint,
								},
							},
						},
					},
				},
			},
			expectedActions: []string{string(Deny), string(Warn)},
			expectedError:   nil,
		},
		{
			name:             "no matching enforcement point",
			enforcementPoint: AuditEnforcementPoint,
			item: map[string]interface{}{
				"spec": map[string]interface{}{
					"scopedEnforcementActions": []apiconstraints.ScopedEnforcementAction{
						{
							Action: string(Deny),
							EnforcementPoints: []apiconstraints.EnforcementPoint{
								{
									Name: WebhookEnforcementPoint,
								},
							},
						},
						{
							Action: string(Warn),
							EnforcementPoints: []apiconstraints.EnforcementPoint{
								{
									Name: WebhookEnforcementPoint,
								},
							},
						},
					},
				},
			},
			expectedActions: []string{},
			expectedError:   nil,
		},
		{
			name:             "wildcard enforcement point",
			enforcementPoint: AuditEnforcementPoint,
			item: map[string]interface{}{
				"spec": map[string]interface{}{
					"scopedEnforcementActions": []apiconstraints.ScopedEnforcementAction{
						{
							Action: string(Deny),
							EnforcementPoints: []apiconstraints.EnforcementPoint{
								{
									Name: AllEnforcementPoints,
								},
							},
						},
						{
							Action: string(Warn),
							EnforcementPoints: []apiconstraints.EnforcementPoint{
								{
									Name: WebhookEnforcementPoint,
								},
							},
						},
					},
				},
			},
			expectedActions: []string{string(Deny)},
			expectedError:   nil,
		},
		{
			name:             "missing scopedEnforcementActions",
			enforcementPoint: AuditEnforcementPoint,
			item: map[string]interface{}{
				"spec": map[string]interface{}{},
			},
			expectedActions: nil,
			expectedError:   nil,
		},
		{
			name:             "invalid scopedEnforcementActions",
			enforcementPoint: AuditEnforcementPoint,
			item: map[string]interface{}{
				"spec": map[string]interface{}{
					"scopedEnforcementActions": "invalid",
				},
			},
			expectedActions: nil,
			expectedError:   errors.New("Could not convert JSON to scopedEnforcementActions: json: cannot unmarshal string into Go value of type []constraints.ScopedEnforcementAction"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actions, err := ScopedActionForEP(tc.enforcementPoint, &unstructured.Unstructured{Object: tc.item})
			if !reflect.DeepEqual(actions, tc.expectedActions) {
				t.Errorf("got ScopedActionForEP() = %v, want %v", actions, tc.expectedActions)
			}
			if err != nil && tc.expectedError != nil && !errors.Is(err, tc.expectedError) && (err.Error() != tc.expectedError.Error()) {
				t.Errorf("got ScopedActionForEP() error = %v, want %v", err, tc.expectedError)
			}
		})
	}
}
