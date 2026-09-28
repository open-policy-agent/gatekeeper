package routing

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// routingClient implements client.Client by embedding the remote cluster client
// and overriding read and write methods to route PodStatus resources
// (status.gatekeeper.sh) to the local cluster.
//
// Get, List, Create, Update, Delete, Patch, and DeleteAllOf route by GVK so
// that PodStatus operations reach the local cluster. Status and SubResource are
// also routed by the parent object's GVK so subresource writes on PodStatus
// objects reach the local cluster instead of the embedded remote cluster
// client.
//
// In non-remote mode, the remote and local clients are the same client.
type routingClient struct {
	// Client is the remote cluster client (reached via --kubeconfig).
	client.Client
	localClusterClient client.Client
	scheme             *runtime.Scheme
}

// Compile-time interface check.
var _ client.Client = &routingClient{}

// NewRoutingClient creates a routing client.Client. In non-remote mode same client for both remote and local clusters.
func NewRoutingClient(remoteCluster, localCluster client.Client, scheme *runtime.Scheme) client.Client {
	if remoteCluster != localCluster {
		log.Info("routing enabled: status.gatekeeper.sh resources route to local cluster client")
	}
	return &routingClient{
		Client:             remoteCluster,
		localClusterClient: localCluster,
		scheme:             scheme,
	}
}

// routeIsLocal resolves the object's GVK and reports whether it belongs to the
// local cluster. An unresolved GVK is a routing failure and without it we cannot
// decide which cluster owns the object, so we return the error.
func routeIsLocal(scheme *runtime.Scheme, obj runtime.Object) (bool, error) {
	gvk, err := resolveGVK(scheme, obj)
	if err != nil {
		return false, fmt.Errorf("routing: cannot determine destination cluster for object: %w", err)
	}
	return isLocalClusterResource(gvk), nil
}

// clientFor returns the correct client for the runtime.Object.
func (r *routingClient) clientFor(obj runtime.Object) (client.Client, error) {
	local, err := routeIsLocal(r.scheme, obj)
	if err != nil {
		return nil, err
	}
	if local {
		return r.localClusterClient, nil
	}
	return r.Client, nil
}

// Reader method overrides.
func (r *routingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c, err := r.clientFor(obj)
	if err != nil {
		return err
	}
	return c.Get(ctx, key, obj, opts...)
}

func (r *routingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	c, err := r.clientFor(list)
	if err != nil {
		return err
	}
	return c.List(ctx, list, opts...)
}

// Writer method overrides.
func (r *routingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	c, err := r.clientFor(obj)
	if err != nil {
		return err
	}
	return c.Create(ctx, obj, opts...)
}

func (r *routingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c, err := r.clientFor(obj)
	if err != nil {
		return err
	}
	return c.Update(ctx, obj, opts...)
}

func (r *routingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	c, err := r.clientFor(obj)
	if err != nil {
		return err
	}
	return c.Delete(ctx, obj, opts...)
}

func (r *routingClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	c, err := r.clientFor(obj)
	if err != nil {
		return err
	}
	return c.Patch(ctx, obj, patch, opts...)
}

func (r *routingClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	c, err := r.clientFor(obj)
	if err != nil {
		return err
	}
	return c.DeleteAllOf(ctx, obj, opts...)
}

// Apply routes by GVK when the apply configuration exposes one.
// Typed apply configurations do not expose their GVK through
// the runtime.ApplyConfiguration interface, so they fall back
// to the remote cluster client. Gatekeeper does not use Apply for
// status.gatekeeper.sh resources today.
func (r *routingClient) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
	if withKind, ok := obj.(interface{ GetObjectKind() schema.ObjectKind }); ok {
		if isLocalClusterResource(withKind.GetObjectKind().GroupVersionKind()) {
			return r.localClusterClient.Apply(ctx, obj, opts...)
		}
	}
	return r.Client.Apply(ctx, obj, opts...)
}

// Status returns a routed SubResourceWriter for the status subresource so that
// status writes on status.gatekeeper.sh objects route to the local cluster
// instead of the embedded remote cluster client.
func (r *routingClient) Status() client.SubResourceWriter {
	return r.SubResource("status")
}

// SubResource returns a routed SubResourceClient.
func (r *routingClient) SubResource(subResource string) client.SubResourceClient {
	return &routingSubResourceClient{
		remoteClusterClient: r.Client.SubResource(subResource),
		localClusterClient:  r.localClusterClient.SubResource(subResource),
		scheme:              r.scheme,
	}
}

// routingSubResourceClient implements client.SubResourceClient by routing each
// operation to the remote or local cluster subresource client based on the
// parent object's GVK.
type routingSubResourceClient struct {
	remoteClusterClient client.SubResourceClient
	localClusterClient  client.SubResourceClient
	scheme              *runtime.Scheme
}

// Compile-time interface check.
var _ client.SubResourceClient = &routingSubResourceClient{}

// clientFor returns the correct subresource client for the parent object.
func (r *routingSubResourceClient) clientFor(obj runtime.Object) (client.SubResourceClient, error) {
	local, err := routeIsLocal(r.scheme, obj)
	if err != nil {
		return nil, err
	}
	if local {
		return r.localClusterClient, nil
	}
	return r.remoteClusterClient, nil
}

func (r *routingSubResourceClient) Get(ctx context.Context, obj, subResource client.Object, opts ...client.SubResourceGetOption) error {
	c, err := r.clientFor(obj)
	if err != nil {
		return err
	}
	return c.Get(ctx, obj, subResource, opts...)
}

func (r *routingSubResourceClient) Create(ctx context.Context, obj, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	c, err := r.clientFor(obj)
	if err != nil {
		return err
	}
	return c.Create(ctx, obj, subResource, opts...)
}

func (r *routingSubResourceClient) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	c, err := r.clientFor(obj)
	if err != nil {
		return err
	}
	return c.Update(ctx, obj, opts...)
}

func (r *routingSubResourceClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	c, err := r.clientFor(obj)
	if err != nil {
		return err
	}
	return c.Patch(ctx, obj, patch, opts...)
}

// Apply routes by GVK when the apply configuration exposes one.
func (r *routingSubResourceClient) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
	if withKind, ok := obj.(interface{ GetObjectKind() schema.ObjectKind }); ok {
		if isLocalClusterResource(withKind.GetObjectKind().GroupVersionKind()) {
			return r.localClusterClient.Apply(ctx, obj, opts...)
		}
	}
	return r.remoteClusterClient.Apply(ctx, obj, opts...)
}
