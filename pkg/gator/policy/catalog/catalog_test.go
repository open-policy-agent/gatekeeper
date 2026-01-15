package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCatalog(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErr     bool
		errContains string
		validate    func(*testing.T, *PolicyCatalog)
	}{
		{
			name: "valid catalog",
			input: `
apiVersion: gator.gatekeeper.sh/v1alpha1
kind: PolicyCatalog
metadata:
  name: test-catalog
  version: v1.0.0
  updatedAt: "2026-01-08T00:00:00Z"
  repository: https://github.com/test/repo
bundles:
  - name: test-bundle
    description: "Test bundle"
    policies:
      - policy1
      - policy2
policies:
  - name: policy1
    version: v1.0.0
    description: "Policy 1"
    category: general
    templatePath: library/policy1/template.yaml
`,
			wantErr: false,
			validate: func(t *testing.T, cat *PolicyCatalog) {
				assert.Equal(t, "gator.gatekeeper.sh/v1alpha1", cat.APIVersion)
				assert.Equal(t, "PolicyCatalog", cat.Kind)
				assert.Equal(t, "test-catalog", cat.Metadata.Name)
				assert.Equal(t, "v1.0.0", cat.Metadata.Version)
				assert.Len(t, cat.Bundles, 1)
				assert.Len(t, cat.Policies, 1)
				assert.Equal(t, "policy1", cat.Policies[0].Name)
			},
		},
		{
			name: "missing apiVersion",
			input: `
kind: PolicyCatalog
metadata:
  name: test-catalog
`,
			wantErr:     true,
			errContains: "missing apiVersion",
		},
		{
			name: "invalid kind",
			input: `
apiVersion: gator.gatekeeper.sh/v1alpha1
kind: InvalidKind
metadata:
  name: test-catalog
`,
			wantErr:     true,
			errContains: "invalid catalog kind",
		},
		{
			name: "missing metadata name",
			input: `
apiVersion: gator.gatekeeper.sh/v1alpha1
kind: PolicyCatalog
metadata:
  version: v1.0.0
`,
			wantErr:     true,
			errContains: "missing metadata.name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat, err := ParseCatalog([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, cat)
			}
		})
	}
}

func TestPolicyCatalog_GetPolicy(t *testing.T) {
	cat := &PolicyCatalog{
		Policies: []Policy{
			{Name: "policy1", Version: "v1.0.0"},
			{Name: "policy2", Version: "v2.0.0"},
		},
	}

	// Found
	p := cat.GetPolicy("policy1")
	require.NotNil(t, p)
	assert.Equal(t, "v1.0.0", p.Version)

	// Not found
	p = cat.GetPolicy("nonexistent")
	assert.Nil(t, p)
}

func TestPolicyCatalog_GetBundle(t *testing.T) {
	cat := &PolicyCatalog{
		Bundles: []Bundle{
			{Name: "bundle1", Description: "Bundle 1"},
			{Name: "bundle2", Description: "Bundle 2"},
		},
	}

	// Found
	b := cat.GetBundle("bundle1")
	require.NotNil(t, b)
	assert.Equal(t, "Bundle 1", b.Description)

	// Not found
	b = cat.GetBundle("nonexistent")
	assert.Nil(t, b)
}

func TestPolicyCatalog_ResolveBundlePolicies(t *testing.T) {
	cat := &PolicyCatalog{
		Bundles: []Bundle{
			{Name: "base", Policies: []string{"p1", "p2"}},
			{Name: "extended", Inherits: "base", Policies: []string{"p3"}},
			{Name: "circular1", Inherits: "circular2", Policies: []string{"pc1"}},
			{Name: "circular2", Inherits: "circular1", Policies: []string{"pc2"}},
		},
	}

	// Simple bundle
	policies, err := cat.ResolveBundlePolicies("base")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"p1", "p2"}, policies)

	// Inherited bundle
	policies, err = cat.ResolveBundlePolicies("extended")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"p1", "p2", "p3"}, policies)

	// Bundle not found
	_, err = cat.ResolveBundlePolicies("nonexistent")
	require.Error(t, err)
	var notFoundErr *BundleNotFoundError
	assert.ErrorAs(t, err, &notFoundErr)

	// Circular inheritance detection
	_, err = cat.ResolveBundlePolicies("circular1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular")
}

func TestHTTPFetcher_FetchFile(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	catalogPath := filepath.Join(tmpDir, "catalog.yaml")
	content := `
apiVersion: gator.gatekeeper.sh/v1alpha1
kind: PolicyCatalog
metadata:
  name: test
  version: v1.0.0
bundles: []
policies: []
`
	err := os.WriteFile(catalogPath, []byte(content), 0o600)
	require.NoError(t, err)

	// Fetch using file:// URL
	fetcher := NewHTTPFetcher(5 * time.Second)
	data, err := fetcher.Fetch(context.Background(), "file://"+catalogPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "gator.gatekeeper.sh/v1alpha1")
}

func TestCache(t *testing.T) {
	// Create temp dir for cache
	tmpDir := t.TempDir()
	t.Setenv("GATOR_HOME", tmpDir)

	cache, err := NewCache()
	require.NoError(t, err)
	assert.Equal(t, tmpDir, cache.Dir())

	// Initially no catalog
	assert.False(t, cache.CatalogExists())

	// Save catalog
	catalogData := []byte(`
apiVersion: gator.gatekeeper.sh/v1
kind: PolicyCatalog
metadata:
  name: cached-test
  version: v1.0.0
bundles: []
policies: []
`)
	err = cache.SaveCatalog(catalogData)
	require.NoError(t, err)

	// Now catalog exists
	assert.True(t, cache.CatalogExists())

	// Load catalog
	cat, err := cache.LoadCatalog()
	require.NoError(t, err)
	assert.Equal(t, "cached-test", cat.Metadata.Name)
}

func TestGetCatalogURL(t *testing.T) {
	// Default URL
	t.Setenv("GATOR_CATALOG_URL", "")
	url := GetCatalogURL()
	assert.Equal(t, DefaultCatalogURL, url)

	// Custom URL
	t.Setenv("GATOR_CATALOG_URL", "https://custom.example.com/catalog.yaml")
	url = GetCatalogURL()
	assert.Equal(t, "https://custom.example.com/catalog.yaml", url)
}

func TestHTTPFetcher_FetchContent(t *testing.T) {
	// Create temp directory with catalog and template
	tmpDir := t.TempDir()

	// Create template file
	templatesDir := filepath.Join(tmpDir, "templates")
	require.NoError(t, os.MkdirAll(templatesDir, 0o755))

	templateContent := `
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
`
	templatePath := filepath.Join(templatesDir, "template.yaml")
	require.NoError(t, os.WriteFile(templatePath, []byte(templateContent), 0o600))

	// Create catalog file
	catalogContent := `
apiVersion: gator.gatekeeper.sh/v1
kind: PolicyCatalog
metadata:
  name: test
  version: v1.0.0
bundles: []
policies:
  - name: k8srequiredlabels
    version: v1.0.0
    description: Test
    category: general
    templatePath: templates/template.yaml
`
	catalogPath := filepath.Join(tmpDir, "catalog.yaml")
	require.NoError(t, os.WriteFile(catalogPath, []byte(catalogContent), 0o600))

	// Fetch catalog first to set baseURL
	fetcher := NewHTTPFetcher(5 * time.Second)
	_, err := fetcher.Fetch(context.Background(), "file://"+catalogPath)
	require.NoError(t, err)

	// Fetch content using relative path
	data, err := fetcher.FetchContent(context.Background(), "templates/template.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(data), "k8srequiredlabels")

	// Fetch content using absolute file:// URL
	data, err = fetcher.FetchContent(context.Background(), "file://"+templatePath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "k8srequiredlabels")
}

func TestLoadCatalog(t *testing.T) {
	// Create temp catalog file
	tmpDir := t.TempDir()
	catalogContent := `
apiVersion: gator.gatekeeper.sh/v1
kind: PolicyCatalog
metadata:
  name: load-test
  version: v2.0.0
  updatedAt: "2026-01-08T00:00:00Z"
  repository: https://github.com/test/repo
bundles:
  - name: test-bundle
    description: "Test bundle"
    policies:
      - policy1
policies:
  - name: policy1
    version: v1.0.0
    description: "Test policy"
    category: general
    templatePath: templates/policy1.yaml
`
	catalogPath := filepath.Join(tmpDir, "catalog.yaml")
	require.NoError(t, os.WriteFile(catalogPath, []byte(catalogContent), 0o600))

	fetcher := NewHTTPFetcher(5 * time.Second)
	cat, err := LoadCatalog(context.Background(), fetcher, "file://"+catalogPath)
	require.NoError(t, err)

	assert.Equal(t, "load-test", cat.Metadata.Name)
	assert.Equal(t, "v2.0.0", cat.Metadata.Version)
	assert.Len(t, cat.Bundles, 1)
	assert.Len(t, cat.Policies, 1)
}

func TestPolicyCatalog_ResolveBundlePolicies_CircularInheritance(t *testing.T) {
	cat := &PolicyCatalog{
		Bundles: []Bundle{
			{Name: "circular1", Inherits: "circular2", Policies: []string{"p1"}},
			{Name: "circular2", Inherits: "nonexistent", Policies: []string{"p2"}},
		},
	}

	// Should error when inherited bundle not found
	_, err := cat.ResolveBundlePolicies("circular1")
	require.Error(t, err)
	var notFoundErr *BundleNotFoundError
	assert.ErrorAs(t, err, &notFoundErr)
	assert.Equal(t, "nonexistent", notFoundErr.Name)
}

func TestPolicyNotFoundError(t *testing.T) {
	err := &PolicyNotFoundError{Name: "missing-policy"}
	assert.Contains(t, err.Error(), "missing-policy")
	assert.Contains(t, err.Error(), "not found")
}

func TestBundleNotFoundError(t *testing.T) {
	err := &BundleNotFoundError{Name: "missing-bundle"}
	assert.Contains(t, err.Error(), "missing-bundle")
	assert.Contains(t, err.Error(), "not found")
}

func TestCache_Dir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("GATOR_HOME", tmpDir)

	cache, err := NewCache()
	require.NoError(t, err)

	// CatalogPath should be inside the cache dir
	assert.Equal(t, filepath.Join(tmpDir, "catalog.yaml"), cache.CatalogPath())
}

func TestNewCache_WithoutGatorHome(t *testing.T) {
	// Clear GATOR_HOME to test default path
	t.Setenv("GATOR_HOME", "")

	cache, err := NewCache()
	require.NoError(t, err)
	assert.NotEmpty(t, cache.Dir())
	assert.True(t, filepath.IsAbs(cache.Dir()))
}

// TestResolveBundlePolicies_ParentFirstOrdering tests that bundle inheritance uses parent-first ordering.
func TestResolveBundlePolicies_ParentFirstOrdering(t *testing.T) {
	cat := &PolicyCatalog{
		Bundles: []Bundle{
			{Name: "grandparent", Policies: []string{"gp1", "gp2"}},
			{Name: "parent", Inherits: "grandparent", Policies: []string{"p1", "p2"}},
			{Name: "child", Inherits: "parent", Policies: []string{"c1", "c2"}},
		},
	}

	// When resolving "child", policies should be in order: grandparent first, then parent, then child
	policies, err := cat.ResolveBundlePolicies("child")
	require.NoError(t, err)

	// Parent-first means grandparent policies come first
	assert.Equal(t, []string{"gp1", "gp2", "p1", "p2", "c1", "c2"}, policies)
}

// TestResolveBundlePolicies_MaxDepth tests that inheritance depth is limited.
func TestResolveBundlePolicies_MaxDepth(t *testing.T) {
	// Create a chain longer than MaxInheritanceDepth
	bundles := make([]Bundle, MaxInheritanceDepth+2)
	for i := 0; i < len(bundles); i++ {
		bundles[i] = Bundle{
			Name:     fmt.Sprintf("bundle%d", i),
			Policies: []string{fmt.Sprintf("policy%d", i)},
		}
		if i > 0 {
			bundles[i].Inherits = bundles[i-1].Name
		}
	}

	cat := &PolicyCatalog{Bundles: bundles}

	// Resolving the last bundle should fail due to depth limit
	_, err := cat.ResolveBundlePolicies(bundles[len(bundles)-1].Name)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maximum depth")
}

// TestResolveBundlePolicies_Deduplication tests that duplicate policies are deduplicated.
func TestResolveBundlePolicies_Deduplication(t *testing.T) {
	cat := &PolicyCatalog{
		Bundles: []Bundle{
			{Name: "parent", Policies: []string{"shared-policy", "parent-only"}},
			{Name: "child", Inherits: "parent", Policies: []string{"shared-policy", "child-only"}},
		},
	}

	policies, err := cat.ResolveBundlePolicies("child")
	require.NoError(t, err)

	// shared-policy should only appear once (from parent, since parent-first)
	assert.Equal(t, []string{"shared-policy", "parent-only", "child-only"}, policies)
}

// TestHTTPFetcher_RejectsPlainHTTP tests that plain HTTP is rejected by default.
func TestHTTPFetcher_RejectsPlainHTTP(t *testing.T) {
	fetcher := NewHTTPFetcher(5 * time.Second)

	// Should reject plain HTTP
	_, err := fetcher.Fetch(context.Background(), "http://example.com/catalog.yaml")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInsecureHTTP)
}

// TestHTTPFetcher_AllowsHTTPWithInsecure tests that HTTP is allowed with insecure flag.
func TestHTTPFetcher_AllowsHTTPWithInsecure(t *testing.T) {
	fetcher := NewHTTPFetcher(5 * time.Second)
	fetcher.SetInsecure(true)

	// With insecure flag, HTTP should be allowed (will fail on network, but not on scheme validation)
	_, err := fetcher.Fetch(context.Background(), "http://localhost:99999/catalog.yaml")
	// Should NOT be ErrInsecureHTTP (it will be a network error instead)
	assert.NotErrorIs(t, err, ErrInsecureHTTP)
}

// TestHTTPFetcher_AllowsHTTPS tests that HTTPS is always allowed.
func TestHTTPFetcher_AllowsHTTPS(t *testing.T) {
	fetcher := NewHTTPFetcher(1 * time.Second)

	// HTTPS should be allowed (will fail on network/cert, but not on scheme validation)
	_, err := fetcher.Fetch(context.Background(), "https://localhost:99999/catalog.yaml")
	// Should NOT be ErrInsecureHTTP
	assert.NotErrorIs(t, err, ErrInsecureHTTP)
}

// TestHTTPFetcher_RejectsPathTraversal tests that path traversal is rejected for HTTP.
func TestHTTPFetcher_RejectsPathTraversal(t *testing.T) {
	fetcher := NewHTTPFetcher(5 * time.Second)
	fetcher.SetBaseURL("https://example.com/catalog.yaml")

	// Should reject paths with ".."
	_, err := fetcher.FetchContent(context.Background(), "../../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

// TestHTTPFetcher_RejectsPathTraversalAbsoluteURL tests path traversal in absolute URLs.
func TestHTTPFetcher_RejectsPathTraversalAbsoluteURL(t *testing.T) {
	fetcher := NewHTTPFetcher(5 * time.Second)

	// Should reject absolute URLs with ".."
	_, err := fetcher.FetchContent(context.Background(), "https://evil.com/../../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}
