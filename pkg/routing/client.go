package routing

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RoutingClient implements client.Client by embedding the target client and
// overriding read and write methods to route PodStatus resources
// (status.gatekeeper.sh) to the management cluster.
//
// Get, List, and all write methods route by GVK so that PodStatus operations
// always target the management cluster regardless of how controller-runtime
// configures cache-backed reads.
//
// In non-remote mode, target and management are the same client.
type RoutingClient struct {
	// Client is the target cluster client
	client.Client
	management client.Client
	scheme     *runtime.Scheme
}

// Compile-time interface check.
var _ client.Client = &RoutingClient{}

// NewRoutingClient creates a RoutingClient. In non-remote mode same client for both target and management.
func NewRoutingClient(target, management client.Client, scheme *runtime.Scheme) *RoutingClient {
	if target != management {
		log.Info("routing enabled: status.gatekeeper.sh resources route to management cluster client")
	}
	return &RoutingClient{
		Client:     target,
		management: management,
		scheme:     scheme,
	}
}

// clientFor returns the correct client for the runtime.Object.
func (r *RoutingClient) clientFor(obj runtime.Object) client.Client {
	gvk, err := ResolveGVK(r.scheme, obj)
	if err != nil {
		log.Error(err, "failed to resolve GVK for write, falling back to target client")
		return r.Client
	}
	if IsManagementResource(gvk) {
		return r.management
	}
	return r.Client
}

// Reader method overrides

func (r *RoutingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return r.clientFor(obj).Get(ctx, key, obj, opts...)
}

func (r *RoutingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return r.clientFor(list).List(ctx, list, opts...)
}

// Writer method overrides

func (r *RoutingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return r.clientFor(obj).Create(ctx, obj, opts...)
}

func (r *RoutingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return r.clientFor(obj).Update(ctx, obj, opts...)
}

func (r *RoutingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return r.clientFor(obj).Delete(ctx, obj, opts...)
}

func (r *RoutingClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return r.clientFor(obj).Patch(ctx, obj, patch, opts...)
}

func (r *RoutingClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	return r.clientFor(obj).DeleteAllOf(ctx, obj, opts...)
}

// Apply always points to the target client
// TODO: Add routing if Gatekeeper ever uses Apply for status.gatekeeper.sh resources.
func (r *RoutingClient) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
	return r.Client.Apply(ctx, obj, opts...)
}
