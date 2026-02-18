package mutation

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"

	externaldataUnversioned "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/unversioned"
	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
)

const (
	clientCert = `
-----BEGIN CERTIFICATE-----
MIID0DCCArigAwIBAgIBATANBgkqhkiG9w0BAQUFADB/MQswCQYDVQQGEwJGUjET
MBEGA1UECAwKU29tZS1TdGF0ZTEOMAwGA1UEBwwFUGFyaXMxDTALBgNVBAoMBERp
bWkxDTALBgNVBAsMBE5TQlUxEDAOBgNVBAMMB0RpbWkgQ0ExGzAZBgkqhkiG9w0B
CQEWDGRpbWlAZGltaS5mcjAeFw0xNDAxMjgyMDM2NTVaFw0yNDAxMjYyMDM2NTVa
MFsxCzAJBgNVBAYTAkZSMRMwEQYDVQQIDApTb21lLVN0YXRlMSEwHwYDVQQKDBhJ
bnRlcm5ldCBXaWRnaXRzIFB0eSBMdGQxFDASBgNVBAMMC3d3dy5kaW1pLmZyMIIB
IjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvpnaPKLIKdvx98KW68lz8pGa
RRcYersNGqPjpifMVjjE8LuCoXgPU0HePnNTUjpShBnynKCvrtWhN+haKbSp+QWX
SxiTrW99HBfAl1MDQyWcukoEb9Cw6INctVUN4iRvkn9T8E6q174RbcnwA/7yTc7p
1NCvw+6B/aAN9l1G2pQXgRdYC/+G6o1IZEHtWhqzE97nY5QKNuUVD0V09dc5CDYB
aKjqetwwv6DFk/GRdOSEd/6bW+20z0qSHpa3YNW6qSp+x5pyYmDrzRIR03os6Dau
ZkChSRyc/Whvurx6o85D6qpzywo8xwNaLZHxTQPgcIA5su9ZIytv9LH2E+lSwwID
AQABo3sweTAJBgNVHRMEAjAAMCwGCWCGSAGG+EIBDQQfFh1PcGVuU1NMIEdlbmVy
YXRlZCBDZXJ0aWZpY2F0ZTAdBgNVHQ4EFgQU+tugFtyN+cXe1wxUqeA7X+yS3bgw
HwYDVR0jBBgwFoAUhMwqkbBrGp87HxfvwgPnlGgVR64wDQYJKoZIhvcNAQEFBQAD
ggEBAIEEmqqhEzeXZ4CKhE5UM9vCKzkj5Iv9TFs/a9CcQuepzplt7YVmevBFNOc0
+1ZyR4tXgi4+5MHGzhYCIVvHo4hKqYm+J+o5mwQInf1qoAHuO7CLD3WNa1sKcVUV
vepIxc/1aHZrG+dPeEHt0MdFfOw13YdUc2FH6AqEdcEL4aV5PXq2eYR8hR4zKbc1
fBtuqUsvA8NWSIyzQ16fyGve+ANf6vXvUizyvwDrPRv/kfvLNa3ZPnLMMxU98Mvh
PXy3PkB8++6U4Y3vdk2Ni2WYYlIls8yqbM4327IKmkDc2TimS8u60CT47mKU7aDY
cbTV5RDkrlaYwm5yqlTIglvCv7o=
-----END CERTIFICATE-----
`
	// nolint:gosec // only used for testing
	clientKey = `
-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAvpnaPKLIKdvx98KW68lz8pGaRRcYersNGqPjpifMVjjE8LuC
oXgPU0HePnNTUjpShBnynKCvrtWhN+haKbSp+QWXSxiTrW99HBfAl1MDQyWcukoE
b9Cw6INctVUN4iRvkn9T8E6q174RbcnwA/7yTc7p1NCvw+6B/aAN9l1G2pQXgRdY
C/+G6o1IZEHtWhqzE97nY5QKNuUVD0V09dc5CDYBaKjqetwwv6DFk/GRdOSEd/6b
W+20z0qSHpa3YNW6qSp+x5pyYmDrzRIR03os6DauZkChSRyc/Whvurx6o85D6qpz
ywo8xwNaLZHxTQPgcIA5su9ZIytv9LH2E+lSwwIDAQABAoIBAFml8cD9a5pMqlW3
f9btTQz1sRL4Fvp7CmHSXhvjsjeHwhHckEe0ObkWTRsgkTsm1XLu5W8IITnhn0+1
iNr+78eB+rRGngdAXh8diOdkEy+8/Cee8tFI3jyutKdRlxMbwiKsouVviumoq3fx
OGQYwQ0Z2l/PvCwy/Y82ffq3ysC5gAJsbBYsCrg14bQo44ulrELe4SDWs5HCjKYb
EI2b8cOMucqZSOtxg9niLN/je2bo/I2HGSawibgcOdBms8k6TvsSrZMr3kJ5O6J+
77LGwKH37brVgbVYvbq6nWPL0xLG7dUv+7LWEo5qQaPy6aXb/zbckqLqu6/EjOVe
ydG5JQECgYEA9kKfTZD/WEVAreA0dzfeJRu8vlnwoagL7cJaoDxqXos4mcr5mPDT
kbWgFkLFFH/AyUnPBlK6BcJp1XK67B13ETUa3i9Q5t1WuZEobiKKBLFm9DDQJt43
uKZWJxBKFGSvFrYPtGZst719mZVcPct2CzPjEgN3Hlpt6fyw3eOrnoECgYEAxiOu
jwXCOmuGaB7+OW2tR0PGEzbvVlEGdkAJ6TC/HoKM1A8r2u4hLTEJJCrLLTfw++4I
ddHE2dLeR4Q7O58SfLphwgPmLDezN7WRLGr7Vyfuv7VmaHjGuC3Gv9agnhWDlA2Q
gBG9/R9oVfL0Dc7CgJgLeUtItCYC31bGT3yhV0MCgYEA4k3DG4L+RN4PXDpHvK9I
pA1jXAJHEifeHnaW1d3vWkbSkvJmgVf+9U5VeV+OwRHN1qzPZV4suRI6M/8lK8rA
Gr4UnM4aqK4K/qkY4G05LKrik9Ev2CgqSLQDRA7CJQ+Jn3Nb50qg6hFnFPafN+J7
7juWln08wFYV4Atpdd+9XQECgYBxizkZFL+9IqkfOcONvWAzGo+Dq1N0L3J4iTIk
w56CKWXyj88d4qB4eUU3yJ4uB4S9miaW/eLEwKZIbWpUPFAn0db7i6h3ZmP5ZL8Q
qS3nQCb9DULmU2/tU641eRUKAmIoka1g9sndKAZuWo+o6fdkIb1RgObk9XNn8R4r
psv+aQKBgB+CIcExR30vycv5bnZN9EFlIXNKaeMJUrYCXcRQNvrnUIUBvAO8+jAe
CdLygS5RtgOLZib0IVErqWsP3EI1ACGuLts0vQ9GFLQGaN1SaMS40C9kvns1mlDu
LhIhYpJ8UsCVt5snWo2N+M+6ANh5tpWdQnEK6zILh4tRbuzaiHgb
-----END RSA PRIVATE KEY-----
`
)

func TestSystem_resolvePlaceholders(t *testing.T) {
	type fields struct {
		providerCache                     *externaldata.ProviderCache
		sendRequestToExternalDataProvider externaldata.SendRequestToProvider
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
				sendRequestToExternalDataProvider: func(_ context.Context, _ *externaldataUnversioned.Provider, _ []string, _ *tls.Certificate) (*externaldata.ProviderResponse, int, error) {
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
					}, http.StatusOK, nil
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
				sendRequestToExternalDataProvider: func(_ context.Context, _ *externaldataUnversioned.Provider, _ []string, _ *tls.Certificate) (*externaldata.ProviderResponse, int, error) {
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
					}, http.StatusOK, nil
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
				sendRequestToExternalDataProvider: func(_ context.Context, _ *externaldataUnversioned.Provider, _ []string, _ *tls.Certificate) (*externaldata.ProviderResponse, int, error) {
					return &externaldata.ProviderResponse{
						Response: externaldata.Response{
							Idempotent:  true,
							SystemError: "system error",
						},
					}, http.StatusOK, nil
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
				sendRequestToExternalDataProvider: func(_ context.Context, _ *externaldataUnversioned.Provider, _ []string, _ *tls.Certificate) (*externaldata.ProviderResponse, int, error) {
					return nil, http.StatusInternalServerError, errors.New("error")
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
				sendRequestToExternalDataProvider: func(_ context.Context, _ *externaldataUnversioned.Provider, _ []string, _ *tls.Certificate) (*externaldata.ProviderResponse, int, error) {
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
					}, http.StatusOK, nil
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
				sendRequestToExternalDataProvider: func(_ context.Context, _ *externaldataUnversioned.Provider, _ []string, _ *tls.Certificate) (*externaldata.ProviderResponse, int, error) {
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
					}, http.StatusOK, nil
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
				sendRequestToExternalDataProvider: func(_ context.Context, _ *externaldataUnversioned.Provider, _ []string, _ *tls.Certificate) (*externaldata.ProviderResponse, int, error) {
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
					}, http.StatusOK, nil
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
	var clientCertWatcher *certwatcher.CertWatcher
	clientCertFile, err := os.CreateTemp("", "client-cert")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(clientCertFile.Name())

	_, err = clientCertFile.WriteString(clientCert)
	if err != nil {
		t.Fatal(err)
	}
	clientCertFile.Close()

	clientKeyFile, err := os.CreateTemp("", "client-key")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(clientKeyFile.Name())

	_, err = clientKeyFile.WriteString(clientKey)
	if err != nil {
		t.Fatal(err)
	}
	clientKeyFile.Close()

	clientCertWatcher, err = certwatcher.New(clientCertFile.Name(), clientKeyFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		_ = clientCertWatcher.Start(context.Background())
	}()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSystem(SystemOpts{
				ProviderCache:                     tt.fields.providerCache,
				SendRequestToExternalDataProvider: tt.fields.sendRequestToExternalDataProvider,
				ClientCertWatcher:                 clientCertWatcher,
			})

			err := s.resolvePlaceholders(context.Background(), tt.args.obj)
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

func TestSystem_getTLSCertificate(t *testing.T) {
	type fields struct {
		clientCertWatcher *certwatcher.CertWatcher
	}
	tests := []struct {
		name    string
		fields  fields
		want    *tls.Certificate
		wantErr bool
	}{
		{
			name: "nil client cert watcher",
			fields: fields{
				clientCertWatcher: nil,
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &System{
				clientCertWatcher: tt.fields.clientCertWatcher,
			}
			got, err := s.getTLSCertificate()
			if (err != nil) != tt.wantErr {
				t.Errorf("System.getTLSCertificate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("System.getTLSCertificate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSystem_sendRequests_contextTimeout(t *testing.T) {
	tests := []struct {
		name            string
		parentTimeout   time.Duration // 0 means use context.Background()
		providerTimeout int
		wantTimeout     time.Duration
	}{
		{
			name:            "uses provider timeout of 10 seconds",
			providerTimeout: 10,
			wantTimeout:     10 * time.Second,
		},
		{
			name:            "uses provider timeout of 3 seconds",
			providerTimeout: 3,
			wantTimeout:     3 * time.Second,
		},
		{
			name:            "uses provider timeout of 1 second",
			providerTimeout: 1,
			wantTimeout:     1 * time.Second,
		},
		{
			name:            "uses default timeout when provider timeout is 0",
			providerTimeout: 0,
			wantTimeout:     defaultExternalDataRequestTimeout,
		},
		{
			name:            "parent context deadline wins when shorter than provider timeout",
			parentTimeout:   1 * time.Second,
			providerTimeout: 10,
			wantTimeout:     1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedCtx context.Context
			providerName := "test-provider-timeout"

			providerCache := externaldata.NewCache()
			provider := &externaldataUnversioned.Provider{
				ObjectMeta: metav1.ObjectMeta{
					Name: providerName,
				},
				Spec: externaldataUnversioned.ProviderSpec{
					URL:      "https://localhost:8080/validate",
					Timeout:  tt.providerTimeout,
					CABundle: util.ValidCABundle,
				},
			}
			if err := providerCache.Upsert(provider); err != nil {
				t.Fatalf("failed to upsert provider: %v", err)
			}

			s := NewSystem(SystemOpts{
				ProviderCache: providerCache,
				SendRequestToExternalDataProvider: func(ctx context.Context, _ *externaldataUnversioned.Provider, _ []string, _ *tls.Certificate) (*externaldata.ProviderResponse, int, error) {
					capturedCtx = ctx
					return &externaldata.ProviderResponse{
						Response: externaldata.Response{Idempotent: true},
					}, http.StatusOK, nil
				},
			})

			parentCtx := context.Background()
			if tt.parentTimeout > 0 {
				var cancel context.CancelFunc
				parentCtx, cancel = context.WithTimeout(parentCtx, tt.parentTimeout)
				defer cancel()
			}

			providerKeys := map[string]sets.Set[string]{
				providerName: sets.New("key1"),
			}
			s.sendRequests(parentCtx, providerKeys, nil)

			if capturedCtx == nil {
				t.Fatal("sendRequestToExternalDataProvider was not called")
			}

			deadline, ok := capturedCtx.Deadline()
			if !ok {
				t.Fatal("expected context to have a deadline")
			}

			// Allow some tolerance for test execution time
			actualTimeout := time.Until(deadline)
			if actualTimeout > tt.wantTimeout || actualTimeout < tt.wantTimeout-250*time.Millisecond {
				t.Errorf("expected timeout ~%v, got %v", tt.wantTimeout, actualTimeout)
			}
		})
	}
}

func TestSystem_sendRequests_parentContextCancellation(t *testing.T) {
	providerName := "test-provider-cancel"

	providerCache := externaldata.NewCache()
	provider := &externaldataUnversioned.Provider{
		ObjectMeta: metav1.ObjectMeta{
			Name: providerName,
		},
		Spec: externaldataUnversioned.ProviderSpec{
			URL:      "https://localhost:8080/validate",
			Timeout:  10,
			CABundle: util.ValidCABundle,
		},
	}
	if err := providerCache.Upsert(provider); err != nil {
		t.Fatalf("failed to upsert provider: %v", err)
	}

	var capturedCtx context.Context
	s := NewSystem(SystemOpts{
		ProviderCache: providerCache,
		SendRequestToExternalDataProvider: func(ctx context.Context, _ *externaldataUnversioned.Provider, _ []string, _ *tls.Certificate) (*externaldata.ProviderResponse, int, error) {
			capturedCtx = ctx
			return &externaldata.ProviderResponse{
				Response: externaldata.Response{Idempotent: true},
			}, http.StatusOK, nil
		},
	})

	parentCtx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	providerKeys := map[string]sets.Set[string]{
		providerName: sets.New[string]("key1"),
	}
	s.sendRequests(parentCtx, providerKeys, nil)

	if capturedCtx == nil {
		t.Fatal("sendRequestToExternalDataProvider was not called")
	}

	if err := capturedCtx.Err(); err != context.Canceled {
		t.Errorf("expected context to be canceled, got %v", err)
	}
}
