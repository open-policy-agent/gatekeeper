package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// CatalogFileName is the name of the cached catalog file.
	CatalogFileName = "catalog.yaml"
	// CatalogSourceFileName is the name of the cached catalog source URL file.
	CatalogSourceFileName = "catalog.source"
	// ConfigFileName is the name of the user config file (future use).
	ConfigFileName = "config.yaml"
)

// Cache manages local caching of the policy catalog.
type Cache struct {
	dir string
}

// NewCache creates a new Cache instance, creating the cache directory if needed.
func NewCache() (*Cache, error) {
	dir := os.Getenv("GATOR_HOME")
	if dir == "" {
		// Use os.UserConfigDir() for cross-platform support
		configDir, err := os.UserConfigDir()
		if err != nil {
			// Fall back to home directory
			home, homeErr := os.UserHomeDir()
			if homeErr != nil {
				return nil, fmt.Errorf("cannot determine config directory: %w", err)
			}
			configDir = filepath.Join(home, ".config")
		}
		dir = filepath.Join(configDir, "gator")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache directory: %w", err)
	}

	return &Cache{dir: dir}, nil
}

// Dir returns the cache directory path.
func (c *Cache) Dir() string {
	return c.dir
}

// CatalogPath returns the path to the cached catalog file.
func (c *Cache) CatalogPath() string {
	return filepath.Join(c.dir, CatalogFileName)
}

// CatalogSourcePath returns the path to the cached catalog source URL file.
func (c *Cache) CatalogSourcePath() string {
	return filepath.Join(c.dir, CatalogSourceFileName)
}

// SaveCatalog saves catalog data to the cache.
func (c *Cache) SaveCatalog(data []byte, sourceURL string) error {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return fmt.Errorf("catalog source URL is required")
	}

	if err := os.WriteFile(c.CatalogPath(), data, 0o600); err != nil {
		return err
	}

	return os.WriteFile(c.CatalogSourcePath(), []byte(sourceURL+"\n"), 0o600)
}

// LoadCatalogData reads the cached catalog data.
func (c *Cache) LoadCatalogData() ([]byte, error) {
	return os.ReadFile(c.CatalogPath())
}

// LoadCatalog reads and parses the cached catalog.
func (c *Cache) LoadCatalog() (*PolicyCatalog, error) {
	data, err := c.LoadCatalogData()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &CatalogNotCachedError{}
		}
		return nil, fmt.Errorf("reading cached catalog: %w", err)
	}
	return ParseCatalog(data)
}

// LoadCatalogSource reads the source URL used to cache the catalog.
func (c *Cache) LoadCatalogSource() (string, error) {
	data, err := os.ReadFile(c.CatalogSourcePath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no cached catalog source found, run 'gator policy update' first")
		}
		return "", fmt.Errorf("reading cached catalog source: %w", err)
	}

	sourceURL := strings.TrimSpace(string(data))
	if sourceURL == "" {
		return "", fmt.Errorf("cached catalog source is empty, run 'gator policy update' first")
	}

	return sourceURL, nil
}

// LoadCatalogWithSource reads and parses the cached catalog and its source URL.
func (c *Cache) LoadCatalogWithSource() (*PolicyCatalog, string, error) {
	cat, err := c.LoadCatalog()
	if err != nil {
		return nil, "", err
	}

	sourceURL, err := c.LoadCatalogSource()
	if err != nil {
		return nil, "", err
	}

	return cat, sourceURL, nil
}

// CatalogExists checks if a cached catalog exists.
func (c *Cache) CatalogExists() bool {
	_, err := os.Stat(c.CatalogPath())
	return err == nil
}

// GetCatalogURL returns the catalog URL from environment or the default.
func GetCatalogURL() string {
	if url := os.Getenv("GATOR_CATALOG_URL"); url != "" {
		return url
	}
	return DefaultCatalogURL
}

// EnsureCatalog loads the catalog from cache, or fetches it if not cached.
func EnsureCatalog(ctx context.Context, cache *Cache, fetcher Fetcher) (*PolicyCatalog, error) {
	if cache.CatalogExists() {
		return cache.LoadCatalog()
	}

	// Fetch and cache
	catalogURL := GetCatalogURL()
	data, err := fetcher.Fetch(ctx, catalogURL)
	if err != nil {
		return nil, fmt.Errorf("fetching catalog: %w", err)
	}

	catalog, err := ParseCatalog(data)
	if err != nil {
		return nil, err
	}

	// Save to cache (warn on failure, non-fatal)
	if err := cache.SaveCatalog(data, catalogURL); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save catalog to cache: %v\n", err)
	}

	return catalog, nil
}

// CatalogNotCachedError is returned when no cached catalog exists.
type CatalogNotCachedError struct{}

func (e *CatalogNotCachedError) Error() string {
	return "no cached catalog found, run 'gator policy update' first"
}
