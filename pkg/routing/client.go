package routing

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// routingClient implements client.Client by embedding the target client and
// overriding read and write methods to route PodStatus resources
// (status.gatekeeper.sh) to the management cluster.
//
// Get, List, Create, Update, Delete, Patch, and DeleteAllOf route by GVK so
// that PodStatus operations target the management cluster.
//
// In non-remote mode, target and management are the same client.
type routingClient struct {
	// Client is the target cluster client
	client.Client
	management client.Client
	scheme     *runtime.Scheme
}

// Compile-time interface check.
var _ client.Client = &routingClient{}

// NewRoutingClient creates a routing client.Client. In non-remote mode same client for both target and management.
func NewRoutingClient(target, management client.Client, scheme *runtime.Scheme) client.Client {
	if target != management {
		log.Info("routing enabled: status.gatekeeper.sh resources route to management cluster client")
	}
	return &routingClient{
		Client:     target,
		management: management,
		scheme:     scheme,
	}
}

// clientFor returns the correct client for the runtime.Object.
func (r *routingClient) clientFor(obj runtime.Object) client.Client {
	gvk, err := resolveGVK(r.scheme, obj)
	if err != nil {
		// Fall back to the target client: the target cluster holds every kind
		// except status.gatekeeper.sh PodStatuses, so it is the correct default
		// for any object whose GVK cannot be resolved.
		log.Error(err, "failed to resolve GVK for write, falling back to target client")
		return r.Client
	}
	if isManagementResource(gvk) {
		return r.management
	}
	return r.Client
}

// Reader method overrides.
func (r *routingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return r.clientFor(obj).Get(ctx, key, obj, opts...)
}

func (r *routingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return r.clientFor(list).List(ctx, list, opts...)
}

// Writer method overrides.
func (r *routingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return r.clientFor(obj).Create(ctx, obj, opts...)
}

func (r *routingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return r.clientFor(obj).Update(ctx, obj, opts...)
}

func (r *routingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return r.clientFor(obj).Delete(ctx, obj, opts...)
}

func (r *routingClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return r.clientFor(obj).Patch(ctx, obj, patch, opts...)
}

func (r *routingClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	return r.clientFor(obj).DeleteAllOf(ctx, obj, opts...)
}

// Apply routes by GVK when the apply configuration exposes one.
// Typed apply configurations do not expose their GVK through
// the runtime.ApplyConfiguration interface, so they fall back
// to the target client. Gatekeeper does not use Apply for status.gatekeeper.sh
// resources today.
func (r *routingClient) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
	if withKind, ok := obj.(interface{ GetObjectKind() schema.ObjectKind }); ok {
		if isManagementResource(withKind.GetObjectKind().GroupVersionKind()) {
			return r.management.Apply(ctx, obj, opts...)
		}
	}
	return r.Client.Apply(ctx, obj, opts...)
}
