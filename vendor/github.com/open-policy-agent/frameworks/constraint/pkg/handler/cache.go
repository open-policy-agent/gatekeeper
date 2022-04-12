package handler

// Cacher is a type - usually a Handler - which needs to cache state.
// Handlers only need implement this interface if they have need of a cache.
// Handlers which do not implement Cacher are assumed to be stateless from
// Client's perspective.
type Cacher interface {
	// GetCache returns the Cache. If nil, the Cacher is treated as having no
	// cache.
	GetCache() Cache
}

// Cache is an interface for Handlers to define which allows them to track
// objects not currently under review. For example, this is required to make
// referential constraints work, or to have Constraint match criteria which
// relies on more than just the object under review.
//
// Implementations must satisfy the per-method requirements for Client to handle
// the Cache properly.
type Cache interface {
	// Add inserts a new object into Cache with identifier key. If an object
	// already exists, replaces the object at key.
	Add(relPath []string, object interface{}) error

	// Remove deletes the object at key from Cache. Deletion succeeds if key
	// does not exist.
	// Remove always succeeds; if for some reason key cannot be deleted the application
	// should panic.
	Remove(relPath []string)
}

type NoCache struct{}

func (n NoCache) Add(relPath []string, object interface{}) error {
	return nil
}

func (n NoCache) Get(relPath []string) (interface{}, error) {
	return nil, nil
}

func (n NoCache) Remove(relPath []string) {}

var _ Cache = NoCache{}
