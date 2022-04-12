package audit

import (
	"context"

	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewAuditCacheLister instantiates a new AuditCache which will read objects in
// watched from auditCache.
func NewAuditCacheLister(auditCache client.Reader, watched *watch.Set) *CacheLister {
	return &CacheLister{
		auditCache: auditCache,
		watched:    watched,
	}
}

// CacheLister lists objects from the audit controller's cache.
type CacheLister struct {
	// auditCache is the cache specifically used for reading objects when
	// auditFromCache is enabled.
	// Caution: only to be read from while watched is locked, such as through
	// DoForEach.
	auditCache client.Reader
	// watched is the set of objects watched by the audit cache.
	watched *watch.Set
}

// ListObjects lists all objects from the audit cache.
func (l *CacheLister) ListObjects(ctx context.Context) ([]unstructured.Unstructured, error) {
	var objs []unstructured.Unstructured
	err := l.watched.DoForEach(func(gvk schema.GroupVersionKind) error {
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
