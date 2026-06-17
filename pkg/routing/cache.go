package routing

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// routingCache implements cache.Cache by routing to either the local cluster
// cache or the remote cluster cache based on the resource GVK. The local
// cluster is where Gatekeeper runs.
// the remote cluster (reached via --kubeconfig) holds everything else.
// In non-remote mode both caches are the same object.
type routingCache struct {
	remoteClusterCache cache.Cache
	localClusterCache  cache.Cache
	scheme             *runtime.Scheme
}

// Compile-time interface check.
var _ cache.Cache = &routingCache{}

// NewRoutingCache creates a routing cache.Cache. In non-remote mode, pass the same cache for both remote and local clusters.
func NewRoutingCache(remoteCluster, localCluster cache.Cache, scheme *runtime.Scheme) cache.Cache {
	if remoteCluster != localCluster {
		log.Info("routing enabled: status.gatekeeper.sh resources route to local cluster cache")
	}
	return &routingCache{
		remoteClusterCache: remoteCluster,
		localClusterCache:  localCluster,
		scheme:             scheme,
	}
}

// cacheFor returns the correct cache for the runtime.Object. An unresolved GVK
// is a routing failure, so return the error rather than risking a misroute of
// a real status.gatekeeper.sh resource to the remote cluster.
func (r *routingCache) cacheFor(obj runtime.Object) (cache.Cache, error) {
	gvk, err := resolveGVK(r.scheme, obj)
	if err != nil {
		return nil, fmt.Errorf("routing: cannot determine destination cluster for object: %w", err)
	}
	return r.cacheForGVK(gvk), nil
}

// cacheForGVK returns the correct cache for GVK.
func (r *routingCache) cacheForGVK(gvk schema.GroupVersionKind) cache.Cache {
	if isLocalClusterResource(gvk) {
		return r.localClusterCache
	}
	return r.remoteClusterCache
}

// client.Reader methods.
func (r *routingCache) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c, err := r.cacheFor(obj)
	if err != nil {
		return err
	}
	return c.Get(ctx, key, obj, opts...)
}

func (r *routingCache) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	c, err := r.cacheFor(list)
	if err != nil {
		return err
	}
	return c.List(ctx, list, opts...)
}

// cache.Informers methods.
func (r *routingCache) GetInformer(ctx context.Context, obj client.Object, opts ...cache.InformerGetOption) (cache.Informer, error) {
	c, err := r.cacheFor(obj)
	if err != nil {
		return nil, err
	}
	return c.GetInformer(ctx, obj, opts...)
}

func (r *routingCache) GetInformerForKind(ctx context.Context, gvk schema.GroupVersionKind, opts ...cache.InformerGetOption) (cache.Informer, error) {
	return r.cacheForGVK(gvk).GetInformerForKind(ctx, gvk, opts...)
}

func (r *routingCache) RemoveInformer(ctx context.Context, obj client.Object) error {
	c, err := r.cacheFor(obj)
	if err != nil {
		return err
	}
	return c.RemoveInformer(ctx, obj)
}

// Start starts both caches. If the local cluster cache fails to start, it
// cancels the remote cluster cache so the manager terminates and Kubernetes can
// restart the pod.
func (r *routingCache) Start(ctx context.Context) error {
	// If both caches are the same (non-remote mode), just start once.
	if r.remoteClusterCache == r.localClusterCache {
		return r.remoteClusterCache.Start(ctx)
	}
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return r.localClusterCache.Start(ctx) })
	g.Go(func() error { return r.remoteClusterCache.Start(ctx) })
	return g.Wait()
}

// WaitForCacheSync waits for both caches to sync.
func (r *routingCache) WaitForCacheSync(ctx context.Context) bool {
	if r.remoteClusterCache == r.localClusterCache {
		return r.remoteClusterCache.WaitForCacheSync(ctx)
	}
	return r.remoteClusterCache.WaitForCacheSync(ctx) && r.localClusterCache.WaitForCacheSync(ctx)
}

// client.FieldIndexer.
func (r *routingCache) IndexField(ctx context.Context, obj client.Object, field string, extractValue client.IndexerFunc) error {
	c, err := r.cacheFor(obj)
	if err != nil {
		return err
	}
	return c.IndexField(ctx, obj, field, extractValue)
}
