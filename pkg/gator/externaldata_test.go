package gator

import (
	"encoding/base64"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/unversioned"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/cert"
)

func testProvider(t *testing.T, name string) *unversioned.Provider {
	t.Helper()
	certPEM, _, err := cert.GenerateSelfSignedCertKey("provider.example.test", nil, nil)
	require.NoError(t, err)
	return &unversioned.Provider{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: unversioned.ProviderSpec{
			URL:      "https://provider.example.test/validate",
			Timeout:  3,
			CABundle: base64.StdEncoding.EncodeToString(certPEM),
		},
	}
}

func TestValidateProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider func(*unversioned.Provider) *unversioned.Provider
		wantErr  string
	}{
		{name: "valid", provider: func(p *unversioned.Provider) *unversioned.Provider { return p }},
		{name: "nil", provider: func(*unversioned.Provider) *unversioned.Provider { return nil }, wantErr: "nil external data Provider"},
		{name: "empty name", provider: func(p *unversioned.Provider) *unversioned.Provider {
			p.Name = ""
			return p
		}, wantErr: "name"},
		{name: "http URL", provider: func(p *unversioned.Provider) *unversioned.Provider {
			p.Spec.URL = "http://provider.example.test/validate"
			return p
		}, wantErr: "HTTPS"},
		{name: "invalid caBundle", provider: func(p *unversioned.Provider) *unversioned.Provider {
			p.Spec.CABundle = "not-base64"
			return p
		}, wantErr: "caBundle"},
		{name: "zero timeout", provider: func(p *unversioned.Provider) *unversioned.Provider {
			p.Spec.Timeout = 0
			return p
		}, wantErr: "spec.timeout must be a positive number of seconds"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProvider(tc.provider(testProvider(t, "provider")))
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestWithExternalDataProviders(t *testing.T) {
	clientOpts, driverArgs, err := WithExternalDataProviders(testProvider(t, "a"), testProvider(t, "b"))()
	require.NoError(t, err)
	require.Empty(t, clientOpts)
	require.Len(t, driverArgs, 1)

	invalid := testProvider(t, "invalid")
	invalid.Spec.Timeout = 0
	tests := []struct {
		name      string
		providers []*unversioned.Provider
		wantErr   string
	}{
		{name: "no Providers", wantErr: "no external data Providers supplied"},
		{name: "nil Provider", providers: []*unversioned.Provider{testProvider(t, "a"), nil}, wantErr: "nil external data Provider"},
		{name: "duplicate", providers: []*unversioned.Provider{testProvider(t, "a"), testProvider(t, "a")}, wantErr: `provider "a" is defined more than once`},
		{name: "invalid Provider", providers: []*unversioned.Provider{invalid}, wantErr: `invalid Provider "invalid"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := WithExternalDataProviders(tc.providers...)()
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
