package mutation

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1alpha1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestSystem_resolvePlaceholders(t *testing.T) {
	type fields struct {
		providerCache                     *externaldata.ProviderCache
		sendRequestToExternalDataProvider types.SendRequestToExternalDataProvider
	}
	type args struct {
		obj *unstructured.Unstructured
	}

	p := &unversioned.ExternalDataPlaceholder{
		Ref: &unversioned.ExternalData{
			Provider:      fakes.ExternalDataProviderName,
			FailurePolicy: types.FailurePolicyFail,
		},
		ValueAtLocation: "bar",
	}

	failurePolicyUseDefault := p.DeepCopy()
	failurePolicyUseDefault.Ref.FailurePolicy = types.FailurePolicyUseDefault
	failurePolicyUseDefault.Ref.Default = "default"

	failurePolicyIgnore := p.DeepCopy()
	failurePolicyIgnore.Ref.FailurePolicy = types.FailurePolicyIgnore

	tests := []struct {
		name          string
		fields        fields
		args          args
		failurePolicy types.ExternalDataFailurePolicy
		want          *unstructured.Unstructured
		wantErr       bool
	}{
		{
			name: "when placeholder is part of a map[string]interface{}",
			fields: fields{
				providerCache: fakes.ExternalDataProviderCache,
				sendRequestToExternalDataProvider: func(ctx context.Context, provider *v1alpha1.Provider, keys []string) (*externaldata.ProviderResponse, error) {
					return &externaldata.ProviderResponse{
						Response: externaldata.Response{
							Idempotent: true,
							Items: []externaldata.Item{
								{
									Key:   "bar",
									Value: "bar-mutated",
								},
							},
						},
					}, nil
				},
			},
			args: args{
				obj: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"foo": p.DeepCopy(),
					},
				},
			},
			want: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"foo": "bar-mutated",
				},
			},
		},
		{
			name: "when placeholder is part of a []interface{}",
			fields: fields{
				providerCache: fakes.ExternalDataProviderCache,
				sendRequestToExternalDataProvider: func(ctx context.Context, provider *v1alpha1.Provider, keys []string) (*externaldata.ProviderResponse, error) {
					return &externaldata.ProviderResponse{
						Response: externaldata.Response{
							Idempotent: true,
							Items: []externaldata.Item{
								{
									Key:   "bar",
									Value: "bar-mutated",
								},
							},
						},
					}, nil
				},
			},
			args: args{
				obj: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"foo": []interface{}{
							map[string]interface{}{
								"baz": p.DeepCopy(),
							},
						},
					},
				},
			},
			want: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"foo": []interface{}{
						map[string]interface{}{
							"baz": "bar-mutated",
						},
					},
				},
			},
		},
		{
			name: "system error",
			fields: fields{
				providerCache: fakes.ExternalDataProviderCache,
				sendRequestToExternalDataProvider: func(ctx context.Context, provider *v1alpha1.Provider, keys []string) (*externaldata.ProviderResponse, error) {
					return &externaldata.ProviderResponse{
						Response: externaldata.Response{
							Idempotent:  true,
							SystemError: "system error",
						},
					}, nil
				},
			},
			args: args{
				obj: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"foo": []interface{}{
							map[string]interface{}{
								"baz": p.DeepCopy(),
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "error when sending request",
			fields: fields{
				providerCache: fakes.ExternalDataProviderCache,
				sendRequestToExternalDataProvider: func(ctx context.Context, provider *v1alpha1.Provider, keys []string) (*externaldata.ProviderResponse, error) {
					return nil, errors.New("error")
				},
			},
			args: args{
				obj: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"foo": []interface{}{
							map[string]interface{}{
								"baz": p.DeepCopy(),
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "failure policy fail",
			fields: fields{
				providerCache: fakes.ExternalDataProviderCache,
				sendRequestToExternalDataProvider: func(ctx context.Context, provider *v1alpha1.Provider, keys []string) (*externaldata.ProviderResponse, error) {
					return &externaldata.ProviderResponse{
						Response: externaldata.Response{
							Idempotent: true,
							Items: []externaldata.Item{
								{
									Key:   "bar",
									Error: "error",
								},
							},
						},
					}, nil
				},
			},
			args: args{
				obj: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"foo": []interface{}{
							map[string]interface{}{
								"baz": p.DeepCopy(),
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "failure policy use default",
			fields: fields{
				providerCache: fakes.ExternalDataProviderCache,
				sendRequestToExternalDataProvider: func(ctx context.Context, provider *v1alpha1.Provider, keys []string) (*externaldata.ProviderResponse, error) {
					return &externaldata.ProviderResponse{
						Response: externaldata.Response{
							Idempotent: true,
							Items: []externaldata.Item{
								{
									Key:   "bar",
									Error: "error",
								},
							},
						},
					}, nil
				},
			},
			args: args{
				obj: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"foo": []interface{}{
							map[string]interface{}{
								"baz": failurePolicyUseDefault,
							},
						},
					},
				},
			},
			want: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"foo": []interface{}{
						map[string]interface{}{
							"baz": "default",
						},
					},
				},
			},
		},
		{
			name: "failure policy ignore",
			fields: fields{
				providerCache: fakes.ExternalDataProviderCache,
				sendRequestToExternalDataProvider: func(ctx context.Context, provider *v1alpha1.Provider, keys []string) (*externaldata.ProviderResponse, error) {
					return &externaldata.ProviderResponse{
						Response: externaldata.Response{
							Idempotent: true,
							Items: []externaldata.Item{
								{
									Key:   "bar",
									Error: "error",
								},
							},
						},
					}, nil
				},
			},
			args: args{
				obj: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"foo": []interface{}{
							map[string]interface{}{
								"baz": failurePolicyIgnore,
							},
						},
					},
				},
			},
			want: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"foo": []interface{}{
						map[string]interface{}{
							"baz": "bar",
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSystem(SystemOpts{
				ProviderCache:                     tt.fields.providerCache,
				SendRequestToExternalDataProvider: tt.fields.sendRequestToExternalDataProvider,
			})
			err := s.resolvePlaceholders(tt.args.obj)
			if (err != nil) != tt.wantErr {
				t.Errorf("System.resolvePlaceholders() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.want != nil && !reflect.DeepEqual(tt.args.obj, tt.want) {
				t.Errorf("System.resolvePlaceholders() = %v, want %v", tt.args.obj, tt.want)
			}
		})
	}
}

func Test_validateExternalDataResponse(t *testing.T) {
	type args struct {
		r *externaldata.ProviderResponse
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "valid response",
			args: args{
				r: &externaldata.ProviderResponse{
					Response: externaldata.Response{
						Idempotent: true,
						Items: []externaldata.Item{
							{
								Key:   "key",
								Value: "value",
							},
						},
					},
				},
			},
		},
		{
			name: "system error",
			args: args{
				r: &externaldata.ProviderResponse{
					Response: externaldata.Response{
						SystemError: "system error",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "not idempotent",
			args: args{
				r: &externaldata.ProviderResponse{
					Response: externaldata.Response{
						Idempotent: false,
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateExternalDataResponse(tt.args.r); (err != nil) != tt.wantErr {
				t.Errorf("validateExternalDataResponse() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
