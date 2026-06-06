package routing

import (
	"context"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// routingCache implements cache.Cache by routing to either a management cluster cache or a target cluster cache based on the resource GVK
// In non-remote mode both caches are the same object.
type routingCache struct {
	target     cache.Cache
	management cache.Cache
	scheme     *runtime.Scheme
}

// Compile-time interface check.
var _ cache.Cache = &routingCache{}

// NewRoutingCache creates a routing cache.Cache. In non-remote mode, pass the same cache for both target and management.
func NewRoutingCache(target, management cache.Cache, scheme *runtime.Scheme) cache.Cache {
	if target != management {
		log.Info("routing enabled: status.gatekeeper.sh resources route to management cluster cache")
	}
	return &routingCache{
		target:     target,
		management: management,
		scheme:     scheme,
	}
}

// cacheFor returns the correct cache for the runtime.Object.
func (r *routingCache) cacheFor(obj runtime.Object) cache.Cache {
	gvk, err := resolveGVK(r.scheme, obj)
	if err != nil {
		// Fall back to the target cache: the target cluster holds every kind
		// except status.gatekeeper.sh PodStatuses, so it is the correct default
		// for any object whose GVK cannot be resolved.
		log.Error(err, "failed to resolve GVK, falling back to target cache")
		return r.target
	}
	return r.cacheForGVK(gvk)
}

// cacheForGVK returns the correct cache for GVK.
func (r *routingCache) cacheForGVK(gvk schema.GroupVersionKind) cache.Cache {
	if isManagementResource(gvk) {
		return r.management
	}
	return r.target
}

// client.Reader methods.
func (r *routingCache) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return r.cacheFor(obj).Get(ctx, key, obj, opts...)
}

func (r *routingCache) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return r.cacheFor(list).List(ctx, list, opts...)
}

// cache.Informers methods.
func (r *routingCache) GetInformer(ctx context.Context, obj client.Object, opts ...cache.InformerGetOption) (cache.Informer, error) {
	return r.cacheFor(obj).GetInformer(ctx, obj, opts...)
}

func (r *routingCache) GetInformerForKind(ctx context.Context, gvk schema.GroupVersionKind, opts ...cache.InformerGetOption) (cache.Informer, error) {
	return r.cacheForGVK(gvk).GetInformerForKind(ctx, gvk, opts...)
}

func (r *routingCache) RemoveInformer(ctx context.Context, obj client.Object) error {
	return r.cacheFor(obj).RemoveInformer(ctx, obj)
}

// Start starts both caches. If the management cache fails to start, it cancels
// the target cache so the manager terminates and Kubernetes can restart the pod.
func (r *routingCache) Start(ctx context.Context) error {
	// If both caches are the same (non-remote mode), just start once.
	if r.target == r.management {
		return r.target.Start(ctx)
	}
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return r.management.Start(ctx) })
	g.Go(func() error { return r.target.Start(ctx) })
	return g.Wait()
}

// WaitForCacheSync waits for both caches to sync.
func (r *routingCache) WaitForCacheSync(ctx context.Context) bool {
	if r.target == r.management {
		return r.target.WaitForCacheSync(ctx)
	}
	return r.target.WaitForCacheSync(ctx) && r.management.WaitForCacheSync(ctx)
}

// client.FieldIndexer.
func (r *routingCache) IndexField(ctx context.Context, obj client.Object, field string, extractValue client.IndexerFunc) error {
	return r.cacheFor(obj).IndexField(ctx, obj, field, extractValue)
}
