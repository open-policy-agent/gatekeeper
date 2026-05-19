package routing

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RoutingCache implements cache.Cache by routing to either a management cluster cache or a target cluster cache based on the resource GVK
// In non-remote mode both caches are the same object.
type RoutingCache struct {
	target     cache.Cache
	management cache.Cache
	scheme     *runtime.Scheme
}

// Compile-time interface check.
var _ cache.Cache = &RoutingCache{}

// NewRoutingCache creates a RoutingCache, In non-remote mode, pass the same cache for both target and management.
func NewRoutingCache(target, management cache.Cache, scheme *runtime.Scheme) *RoutingCache {
	if target != management {
		log.Info("routing enabled: status.gatekeeper.sh resources route to management cluster cache")
	}
	return &RoutingCache{
		target:     target,
		management: management,
		scheme:     scheme,
	}
}

// cacheFor returns the correct cache for the runtime.Object.
func (r *RoutingCache) cacheFor(obj runtime.Object) cache.Cache {
	gvk, err := ResolveGVK(r.scheme, obj)
	if err != nil {
		log.Error(err, "failed to resolve GVK, falling back to target cache")
		return r.target
	}
	if IsManagementResource(gvk) {
		return r.management
	}
	return r.target
}

// cacheForGVK returns the corect cache for GVK.
func (r *RoutingCache) cacheForGVK(gvk schema.GroupVersionKind) cache.Cache {
	if IsManagementResource(gvk) {
		return r.management
	}
	return r.target
}

// client.Reader methods

func (r *RoutingCache) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return r.cacheFor(obj).Get(ctx, key, obj, opts...)
}

func (r *RoutingCache) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return r.cacheFor(list).List(ctx, list, opts...)
}

// cache.Informers methods

func (r *RoutingCache) GetInformer(ctx context.Context, obj client.Object, opts ...cache.InformerGetOption) (cache.Informer, error) {
	return r.cacheFor(obj).GetInformer(ctx, obj, opts...)
}

func (r *RoutingCache) GetInformerForKind(ctx context.Context, gvk schema.GroupVersionKind, opts ...cache.InformerGetOption) (cache.Informer, error) {
	return r.cacheForGVK(gvk).GetInformerForKind(ctx, gvk, opts...)
}

func (r *RoutingCache) RemoveInformer(ctx context.Context, obj client.Object) error {
	return r.cacheFor(obj).RemoveInformer(ctx, obj)
}

// Start starts both caches.
func (r *RoutingCache) Start(ctx context.Context) error {
	// If both caches are the same (non-remote mode), just start once
	if r.target == r.management {
		return r.target.Start(ctx)
	}
	go func() {
		if err := r.management.Start(ctx); err != nil {
			log.Error(err, "management cache failed to start")
		}
	}()
	return r.target.Start(ctx)
}

// WaitForCacheSync waits for both caches to sync.
func (r *RoutingCache) WaitForCacheSync(ctx context.Context) bool {
	if r.target == r.management {
		return r.target.WaitForCacheSync(ctx)
	}
	return r.target.WaitForCacheSync(ctx) && r.management.WaitForCacheSync(ctx)
}

// client.FieldIndexer

func (r *RoutingCache) IndexField(ctx context.Context, obj client.Object, field string, extractValue client.IndexerFunc) error {
	return r.cacheFor(obj).IndexField(ctx, obj, field, extractValue)
}
