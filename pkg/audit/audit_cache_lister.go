package audit

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewAuditCacheLister instantiates a new AuditCache which will read objects in
// watched from auditCache.
func NewAuditCacheLister(auditCache client.Reader, lister WatchIterator) *CacheLister {
	return &CacheLister{
		auditCache:    auditCache,
		watchIterator: lister,
	}
}

// CacheLister lists objects from the audit controller's cache.
type CacheLister struct {
	// auditCache is the cache specifically used for reading objects when
	// auditFromCache is enabled.
	// Caution: only to be read from while watched is locked, such as through
	// DoForEach.
	auditCache client.Reader
	// watchIterator is a delegate like CacheManager that we can use to query a watched set of GKVs.
	// Passing our logic as a callback to a watched.Set allows us to take actions while
	// holding the lock on the watched.Set. This prevents us from querying the API server
	// for kinds that aren't currently being watched by the CacheManager.
	watchIterator WatchIterator
}

// wraps DoForEach from a watch.Set.
type WatchIterator interface {
	DoForEach(listFunc func(gvk schema.GroupVersionKind) error) error
}

// ListObjects lists all objects from the audit cache.
func (l *CacheLister) ListObjects(ctx context.Context) ([]unstructured.Unstructured, error) {
	var objs []unstructured.Unstructured
	err := l.watchIterator.DoForEach(func(gvk schema.GroupVersionKind) error {
		gvkObjects, err := listObjects(ctx, l.auditCache, gvk)
		if err != nil {
			return err
		}

		objs = append(objs, gvkObjects...)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return objs, nil
}

func listObjects(ctx context.Context, reader client.Reader, gvk schema.GroupVersionKind) ([]unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{
		Object: map[string]interface{}{},
		Items:  []unstructured.Unstructured{},
	}

	gvk.Kind += "List"
	list.SetGroupVersionKind(gvk)

	err := reader.List(ctx, list)
	if err != nil {
		return nil, err
	}

	return list.Items, nil
}
