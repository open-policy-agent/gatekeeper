package catalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPFetcher_PathTraversalProtection(t *testing.T) {
	// Create a temp directory with a test file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "subdir", "test.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(testFile), 0o755))
	require.NoError(t, os.WriteFile(testFile, []byte("test: content"), 0o600))

	// Create a fetcher with the subdir as base URL
	fetcher := NewHTTPFetcher(DefaultTimeout)
	subdirURL := "file://" + filepath.Join(tempDir, "subdir") + "/catalog.yaml"
	fetcher.SetBaseURL(subdirURL)

	ctx := context.Background()

	// Valid access within base directory should work
	content, err := fetcher.FetchContent(ctx, "test.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(content), "test: content")

	// Path traversal attempt should fail (early check for "..")
	_, err = fetcher.FetchContent(ctx, "../../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")

	// Another traversal attempt
	_, err = fetcher.FetchContent(ctx, "subdir/../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestHTTPFetcher_SetBaseURL(t *testing.T) {
	fetcher := NewHTTPFetcher(DefaultTimeout)

	// Test setting base URL
	fetcher.SetBaseURL("https://example.com/policies/catalog.yaml")

	// Use RLock to safely read baseURL for testing
	fetcher.mu.RLock()
	baseURL := fetcher.baseURL
	fetcher.mu.RUnlock()
	assert.Equal(t, "https://example.com/policies/catalog.yaml", baseURL)

	// Test with file:// URL
	fetcher.SetBaseURL("file:///tmp/policies/catalog.yaml")
	fetcher.mu.RLock()
	baseURL = fetcher.baseURL
	fetcher.mu.RUnlock()
	assert.Equal(t, "file:///tmp/policies/catalog.yaml", baseURL)
}

func TestHTTPFetcher_ResolveContentURL(t *testing.T) {
	fetcher := NewHTTPFetcher(DefaultTimeout)

	// Test with HTTPS base URL
	fetcher.SetBaseURL("https://example.com/policies/catalog.yaml")
	resolved, err := fetcher.resolveContentURL("template.yaml")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/policies/template.yaml", resolved)

	// Test with subdirectory path
	resolved, err = fetcher.resolveContentURL("library/general/template.yaml")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/policies/library/general/template.yaml", resolved)
}

func TestNewHTTPFetcher(t *testing.T) {
	fetcher := NewHTTPFetcher(5 * time.Second)
	require.NotNil(t, fetcher)
	require.NotNil(t, fetcher.client)
	assert.Equal(t, 5*time.Second, fetcher.client.Timeout)

	fetcher.mu.RLock()
	baseURL := fetcher.baseURL
	fetcher.mu.RUnlock()
	assert.Empty(t, baseURL)
}

func TestNewHTTPFetcherWithBaseURL(t *testing.T) {
	baseURL := "https://example.com/catalog.yaml"
	fetcher := NewHTTPFetcherWithBaseURL(DefaultTimeout, baseURL)
	require.NotNil(t, fetcher)
	require.NotNil(t, fetcher.client)

	fetcher.mu.RLock()
	actualURL := fetcher.baseURL
	fetcher.mu.RUnlock()
	assert.Equal(t, baseURL, actualURL)
}
