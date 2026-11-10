package reader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator"
	"github.com/stretchr/testify/require"
)

func TestReadProviders(t *testing.T) {
	const valid = `apiVersion: externaldata.gatekeeper.sh/v1beta1
kind: Provider
metadata:
  name: test-provider
spec:
  url: https://provider.example.test/validate
  timeout: 3
`
	tests := []struct {
		name      string
		input     string
		wantErr   string
		wantErrIs error
	}{
		{name: "v1beta1", input: valid},
		{name: "v1alpha1", input: strings.Replace(valid, "v1beta1", "v1alpha1", 1)},
		{name: "invalid kind", input: strings.Replace(valid, "kind: Provider", "kind: Config", 1), wantErr: "expected externaldata.gatekeeper.sh", wantErrIs: gator.ErrNotAProvider},
		{name: "invalid version", input: strings.Replace(valid, "v1beta1", "v2", 1), wantErr: "expected externaldata.gatekeeper.sh", wantErrIs: gator.ErrNotAProvider},
		{name: "missing name", input: strings.Replace(valid, "name: test-provider", "name: \"\"", 1), wantErr: "metadata.name must not be empty"},
		{name: "namespaced", input: strings.Replace(valid, "name: test-provider", "name: test-provider\n  namespace: default", 1), wantErr: "must be cluster-scoped"},
		{name: "bad field type", input: strings.Replace(valid, "timeout: 3", "timeout: invalid", 1), wantErr: "adding provider", wantErrIs: gator.ErrAddingProvider},
		{name: "mixed documents", input: valid + "---\napiVersion: v1\nkind: Pod\nmetadata:\n  name: pod\n", wantErr: "expected externaldata.gatekeeper.sh", wantErrIs: gator.ErrNotAProvider},
		{name: "duplicate documents", input: valid + "---\n" + valid, wantErr: "already defined"},
		{name: "empty document", input: "---\n", wantErr: "no Provider resources"},
		{name: "invalid yaml", input: "metadata: [\n", wantErr: "reading Provider files"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "providers.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.input), 0o600))
			providers, err := ReadProviders([]string{path})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				if tt.wantErrIs != nil {
					require.ErrorIs(t, err, tt.wantErrIs)
				}
				return
			}
			require.NoError(t, err)
			require.Len(t, providers, 1)
			require.Equal(t, "test-provider", providers[0].Resource.Name)
			require.Equal(t, "https://provider.example.test/validate", providers[0].Resource.Spec.URL)
			require.Equal(t, path, providers[0].Source)
		})
	}
}

func TestReadProvidersPaths(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.yaml")
	second := filepath.Join(dir, "second.json")
	require.NoError(t, os.WriteFile(first, []byte(`apiVersion: externaldata.gatekeeper.sh/v1beta1
kind: Provider
metadata:
  name: first
`), 0o600))
	require.NoError(t, os.WriteFile(second, []byte(`{"apiVersion":"externaldata.gatekeeper.sh/v1beta1","kind":"Provider","metadata":{"name":"second"}}`), 0o600))

	providers, err := ReadProviders([]string{dir})
	require.NoError(t, err)
	require.Len(t, providers, 2)

	_, err = ReadProviders([]string{first, dir})
	require.ErrorContains(t, err, "already defined")

	_, err = ReadProviders([]string{filepath.Join(dir, "missing.yaml")})
	require.ErrorContains(t, err, "reading Provider files")

	_, err = ReadProviders([]string{""})
	require.Error(t, err)
}
