package catalog

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"sigs.k8s.io/yaml"
)

// DefaultCatalogURL is the default URL for the policy catalog.
const (
	DefaultCatalogURL = "https://raw.githubusercontent.com/open-policy-agent/gatekeeper-library/master/catalog.yaml"

	// DefaultTimeout is the default timeout for HTTP requests.
	DefaultTimeout = 30 * time.Second

	// fileScheme is the URL scheme for local files.
	fileScheme = "file"
)

// Fetcher defines the interface for fetching catalog data.
type Fetcher interface {
	// Fetch retrieves the catalog from the given URL.
	Fetch(ctx context.Context, catalogURL string) ([]byte, error)
	// FetchContent retrieves content from a URL (for templates/constraints).
	FetchContent(ctx context.Context, contentURL string) ([]byte, error)
}

// HTTPFetcher implements Fetcher using HTTP and file:// protocols.
// HTTPFetcher is safe for concurrent use after creation.
type HTTPFetcher struct {
	client  *http.Client
	baseURL string
	mu      sync.RWMutex // protects baseURL
}

// NewHTTPFetcher creates a new HTTPFetcher with the given timeout.
func NewHTTPFetcher(timeout time.Duration) *HTTPFetcher {
	return &HTTPFetcher{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// NewHTTPFetcherWithBaseURL creates an HTTPFetcher with a preset base URL.
// Use this when the catalog is loaded from cache but content needs to be fetched.
func NewHTTPFetcherWithBaseURL(timeout time.Duration, baseURL string) *HTTPFetcher {
	return &HTTPFetcher{
		client: &http.Client{
			Timeout: timeout,
		},
		baseURL: baseURL,
	}
}

// SetBaseURL sets the base URL for resolving relative paths.
func (f *HTTPFetcher) SetBaseURL(baseURL string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.baseURL = baseURL
}

// Fetch retrieves the catalog from the given URL.
func (f *HTTPFetcher) Fetch(ctx context.Context, catalogURL string) ([]byte, error) {
	u, err := url.Parse(catalogURL)
	if err != nil {
		return nil, fmt.Errorf("parsing catalog URL: %w", err)
	}

	// Store base URL for relative path resolution
	f.mu.Lock()
	f.baseURL = catalogURL
	f.mu.Unlock()

	if u.Scheme == fileScheme {
		return os.ReadFile(u.Path)
	}

	return f.fetchHTTP(ctx, catalogURL)
}

// FetchContent retrieves content from a URL, resolving relative paths against the catalog URL.
func (f *HTTPFetcher) FetchContent(ctx context.Context, contentPath string) ([]byte, error) {
	// Check if it's an absolute URL
	u, err := url.Parse(contentPath)
	if err != nil {
		return nil, fmt.Errorf("parsing content path: %w", err)
	}

	// If it has a scheme, fetch directly
	if u.Scheme != "" {
		if u.Scheme == fileScheme {
			return os.ReadFile(u.Path)
		}
		return f.fetchHTTP(ctx, contentPath)
	}

	// Get base URL with read lock
	f.mu.RLock()
	baseURLCopy := f.baseURL
	f.mu.RUnlock()

	// Check if base URL is a file:// URL - resolve locally
	baseU, _ := url.Parse(baseURLCopy)
	if baseU != nil && baseU.Scheme == fileScheme {
		// For file:// base URLs, resolve path relative to catalog directory
		catalogDir := filepath.Dir(baseU.Path)
		fullPath := filepath.Join(catalogDir, contentPath)

		// Security: Validate the resolved path is within the catalog directory
		// to prevent path traversal attacks (e.g., ../../../etc/passwd)
		cleanPath := filepath.Clean(fullPath)
		cleanBase := filepath.Clean(catalogDir)
		if !strings.HasPrefix(cleanPath, cleanBase+string(filepath.Separator)) && cleanPath != cleanBase {
			return nil, fmt.Errorf("content path escapes catalog directory: %s", contentPath)
		}

		return os.ReadFile(fullPath)
	}

	// Otherwise, resolve against HTTP base URL
	contentURL, err := f.resolveContentURL(contentPath)
	if err != nil {
		return nil, err
	}

	return f.fetchHTTP(ctx, contentURL)
}

func (f *HTTPFetcher) fetchHTTP(ctx context.Context, targetURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", targetURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: status %d", targetURL, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (f *HTTPFetcher) resolveContentURL(relativePath string) (string, error) {
	f.mu.RLock()
	baseURLCopy := f.baseURL
	f.mu.RUnlock()

	if baseURLCopy == "" {
		return "", fmt.Errorf("no base URL set for resolving relative path: %s", relativePath)
	}

	baseU, err := url.Parse(baseURLCopy)
	if err != nil {
		return "", fmt.Errorf("parsing base URL: %w", err)
	}

	// Get the directory containing the catalog file and add trailing slash
	// The trailing slash is required for url.Parse to correctly append relative paths
	// Use path.Dir (not filepath.Dir) for URL paths to ensure cross-platform compatibility
	baseU.Path = path.Dir(baseU.Path) + "/"

	// Construct content URL
	contentU, err := baseU.Parse(relativePath)
	if err != nil {
		return "", fmt.Errorf("resolving content path: %w", err)
	}

	return contentU.String(), nil
}

// LoadCatalog fetches and parses a catalog from the given URL.
func LoadCatalog(ctx context.Context, fetcher Fetcher, catalogURL string) (*PolicyCatalog, error) {
	data, err := fetcher.Fetch(ctx, catalogURL)
	if err != nil {
		return nil, fmt.Errorf("fetching catalog: %w", err)
	}

	return ParseCatalog(data)
}

// ParseCatalog parses catalog data from YAML bytes.
func ParseCatalog(data []byte) (*PolicyCatalog, error) {
	var catalog PolicyCatalog
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("parsing catalog YAML: %w", err)
	}

	// Validate required fields
	if catalog.APIVersion == "" {
		return nil, fmt.Errorf("catalog missing apiVersion")
	}
	if catalog.Kind != "PolicyCatalog" {
		return nil, fmt.Errorf("invalid catalog kind: expected PolicyCatalog, got %s", catalog.Kind)
	}
	if catalog.Metadata.Name == "" {
		return nil, fmt.Errorf("catalog missing metadata.name")
	}

	return &catalog, nil
}
