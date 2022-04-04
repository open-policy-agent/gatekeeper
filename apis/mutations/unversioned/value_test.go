package unversioned

import (
	"errors"
	"reflect"
	"testing"

	"github.com/open-policy-agent/gatekeeper/pkg/externaldata"
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
			name: "double-defined 1",
			obj: &AssignField{
				Value: &types.Anything{Value: "something"},
				FromMetadata: &FromMetadata{
					Field: ObjNamespace,
				},
			},
			wantErr: ErrInvalidAssignField,
		},
		{
			name: "double-defined 2",
			obj: &AssignField{
				Value: &types.Anything{Value: "something"},
				ExternalData: &ExternalData{
					Provider:   "some-provider",
					DataSource: types.DataSourceValueAtLocation,
				},
			},
			wantErr: ErrInvalidAssignField,
		},
		{
			name: "double-defined 3",
			obj: &AssignField{
				FromMetadata: &FromMetadata{
					Field: ObjNamespace,
				},
				ExternalData: &ExternalData{
					Provider:   "some-provider",
					DataSource: types.DataSourceValueAtLocation,
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
		expected interface{}
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
		{
			name:    "retrieve external data placeholder",
			objNS:   "some-namespace",
			objName: "some-name",
			assign: &AssignField{
				ExternalData: &ExternalData{
					Provider:   "some-provider",
					DataSource: types.DataSourceValueAtLocation,
				},
			},
			expected: &ExternalDataPlaceholder{
				Ref: &ExternalData{
					Provider:   "some-provider",
					DataSource: types.DataSourceValueAtLocation,
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{}
			obj.SetName(tc.objName)
			obj.SetNamespace(tc.objNS)
			v, err := tc.assign.GetValue(&types.Mutable{Object: obj})
			if err != nil {
				t.Errorf("error getting value: %v", err)
			}
			if !reflect.DeepEqual(v, tc.expected) {
				t.Errorf("assign.GetValue() = %v; want %v", v, tc.expected)
			}
		})
	}
}

func TestExternalData_Validate(t *testing.T) {
	type fields struct {
		Provider      string
		DataSource    types.ExternalDataSource
		FailurePolicy types.ExternalDataFailurePolicy
		Default       string
	}
	tests := []struct {
		name               string
		fields             fields
		disableFeatureFlag bool
		wantErr            error
	}{
		{
			name: "valid",
			fields: fields{
				Provider:      "provider",
				DataSource:    types.DataSourceValueAtLocation,
				FailurePolicy: types.FailurePolicyFail,
			},
		},
		{
			name: "valid with default",
			fields: fields{
				Provider:      "provider",
				DataSource:    types.DataSourceValueAtLocation,
				FailurePolicy: types.FailurePolicyUseDefault,
				Default:       "default",
			},
		},
		{
			name: "no default",
			fields: fields{
				FailurePolicy: types.FailurePolicyUseDefault,
			},
			wantErr: ErrExternalDataNoDefault,
		},
		{
			name:               "disabled feature flag",
			disableFeatureFlag: true,
			wantErr:            ErrExternalDataFeatureFlag,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.disableFeatureFlag {
				*externaldata.ExternalDataEnabled = true
				defer func() {
					*externaldata.ExternalDataEnabled = false
				}()
			}
			e := &ExternalData{
				Provider:      tt.fields.Provider,
				DataSource:    tt.fields.DataSource,
				FailurePolicy: tt.fields.FailurePolicy,
				Default:       tt.fields.Default,
			}
			if err := e.Validate(); err != nil && !errors.Is(tt.wantErr, err) {
				t.Errorf("ExternalData.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
