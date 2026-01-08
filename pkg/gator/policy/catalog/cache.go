package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// CatalogFileName is the name of the cached catalog file.
	CatalogFileName = "catalog.yaml"
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

// SaveCatalog saves catalog data to the cache.
func (c *Cache) SaveCatalog(data []byte) error {
	return os.WriteFile(c.CatalogPath(), data, 0o600)
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

	// Save to cache (best effort)
	_ = cache.SaveCatalog(data)

	return catalog, nil
}

// CatalogNotCachedError is returned when no cached catalog exists.
type CatalogNotCachedError struct{}

func (e *CatalogNotCachedError) Error() string {
	return "no cached catalog found, run 'gator policy update' first"
}
