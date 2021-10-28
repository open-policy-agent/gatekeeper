package unversioned

import (
	"errors"
	"testing"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestAssignField_Validate(t *testing.T) {
	tests := []struct {
		name    string
		obj     *AssignField
		wantErr error
	}{
		{
			name: "valid constant",
			obj: &AssignField{
				Value: &types.Anything{Value: "something"},
			},
			wantErr: nil,
		},
		{
			name: "valid metadata: name",
			obj: &AssignField{
				FromMetadata: &FromMetadata{
					Field: ObjName,
				},
			},
			wantErr: nil,
		},
		{
			name: "valid metadata: namespace",
			obj: &AssignField{
				FromMetadata: &FromMetadata{
					Field: ObjNamespace,
				},
			},
			wantErr: nil,
		},
		{
			name: "invalid metadata: fish",
			obj: &AssignField{
				FromMetadata: &FromMetadata{
					Field: "fish",
				},
			},
			wantErr: ErrInvalidFromMetadata,
		},
		{
			name:    "empty object",
			obj:     &AssignField{},
			wantErr: ErrInvalidAssignField,
		},
		{
			name: "double-defined",
			obj: &AssignField{
				Value: &types.Anything{Value: "something"},
				FromMetadata: &FromMetadata{
					Field: ObjNamespace,
				},
			},
			wantErr: ErrInvalidAssignField,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.obj.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err := `%v`, wanted `%v`", err, tc.wantErr)
			}
		})
	}
}

func TestAssignField_GetValue(t *testing.T) {
	tests := []struct {
		name     string
		objNS    string
		objName  string
		assign   *AssignField
		expected string
	}{
		{
			name:    "retrieve constant",
			objNS:   "some-namespace",
			objName: "some-name",
			assign: &AssignField{
				Value: &types.Anything{Value: "something"},
			},
			expected: "something",
		},
		{
			name:    "retrieve namespace",
			objNS:   "some-namespace",
			objName: "some-name",
			assign: &AssignField{
				FromMetadata: &FromMetadata{
					Field: ObjNamespace,
				},
			},
			expected: "some-namespace",
		},
		{
			name:    "retrieve name",
			objNS:   "some-namespace",
			objName: "some-name",
			assign: &AssignField{
				FromMetadata: &FromMetadata{
					Field: ObjName,
				},
			},
			expected: "some-name",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{}
			obj.SetName(tc.objName)
			obj.SetNamespace(tc.objNS)
			v, err := tc.assign.GetValue(obj)
			if err != nil {
				t.Errorf("error getting value: %v", err)
			}
			if v != tc.expected {
				t.Errorf("assign.GetValue() = %v; want %v", v, tc.expected)
			}
		})
	}
}
