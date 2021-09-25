package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestValidation(t *testing.T) {
	tests := []struct {
		name        string
		obj         *AssignField
		errExpected bool
	}{
		{
			name: "valid constant",
			obj: &AssignField{
				Value: &Anything{Value: "something"},
			},
			errExpected: false,
		},
		{
			name: "valid metadata: name",
			obj: &AssignField{
				FromMetadata: &FromMetadata{
					Field: ObjName,
				},
			},
			errExpected: false,
		},
		{
			name: "valid metadata: namespace",
			obj: &AssignField{
				FromMetadata: &FromMetadata{
					Field: ObjNamespace,
				},
			},
			errExpected: false,
		},
		{
			name: "invalid metadata: fish",
			obj: &AssignField{
				FromMetadata: &FromMetadata{
					Field: "fish",
				},
			},
			errExpected: true,
		},
		{
			name:        "empty object",
			obj:         &AssignField{},
			errExpected: true,
		},
		{
			name: "double-defined",
			obj: &AssignField{
				Value: &Anything{Value: "something"},
				FromMetadata: &FromMetadata{
					Field: ObjNamespace,
				},
			},
			errExpected: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.obj.Validate()
			hasErr := err != nil
			if hasErr != tc.errExpected {
				t.Errorf("err := %v, wanted existence to be %v", err, tc.errExpected)
			}
		})
	}
}

func TestValueRetrieval(t *testing.T) {
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
				Value: &Anything{Value: "something"},
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
