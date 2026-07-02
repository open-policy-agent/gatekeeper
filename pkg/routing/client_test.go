package routing

import (
	"context"
	"errors"
	"testing"

	statusv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1alpha1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// fakeClient records which methods were called. It embeds the client.Client
// interface (nil) so any method not overridden here panics if called, surfacing
// accidental usage.
type fakeClient struct {
	client.Client
	got          bool
	listed       bool
	created      bool
	updated      bool
	deleted      bool
	patched      bool
	deletedAllOf bool
	applied      bool
	// subresource call tracking
	subGot     bool
	subCreated bool
	subUpdated bool
	subPatched bool
	subApplied bool
}

func (f *fakeClient) Get(_ context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
	f.got = true
	return nil
}

func (f *fakeClient) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	f.listed = true
	return nil
}

func (f *fakeClient) Create(_ context.Context, _ client.Object, _ ...client.CreateOption) error {
	f.created = true
	return nil
}

func (f *fakeClient) Update(_ context.Context, _ client.Object, _ ...client.UpdateOption) error {
	f.updated = true
	return nil
}

func (f *fakeClient) Delete(_ context.Context, _ client.Object, _ ...client.DeleteOption) error {
	f.deleted = true
	return nil
}

func (f *fakeClient) Patch(_ context.Context, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
	f.patched = true
	return nil
}

func (f *fakeClient) DeleteAllOf(_ context.Context, _ client.Object, _ ...client.DeleteAllOfOption) error {
	f.deletedAllOf = true
	return nil
}

func (f *fakeClient) Apply(_ context.Context, _ runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
	f.applied = true
	return nil
}

// SubResource returns a fake SubResourceClient that records calls back onto the
// parent fakeClient, so tests can assert which underlying client a routed
// subresource operation reached.
func (f *fakeClient) SubResource(_ string) client.SubResourceClient {
	return &fakeSubResourceClient{parent: f}
}

// fakeSubResourceClient records subresource calls onto its parent fakeClient. It
// embeds the SubResourceClient interface (nil) so any method not overridden here
// panics if called, surfacing accidental usage.
type fakeSubResourceClient struct {
	client.SubResourceClient
	parent *fakeClient
}

func (f *fakeSubResourceClient) Get(_ context.Context, _, _ client.Object, _ ...client.SubResourceGetOption) error {
	f.parent.subGot = true
	return nil
}

func (f *fakeSubResourceClient) Create(_ context.Context, _, _ client.Object, _ ...client.SubResourceCreateOption) error {
	f.parent.subCreated = true
	return nil
}

func (f *fakeSubResourceClient) Update(_ context.Context, _ client.Object, _ ...client.SubResourceUpdateOption) error {
	f.parent.subUpdated = true
	return nil
}

func (f *fakeSubResourceClient) Patch(_ context.Context, _ client.Object, _ client.Patch, _ ...client.SubResourcePatchOption) error {
	f.parent.subPatched = true
	return nil
}

func (f *fakeSubResourceClient) Apply(_ context.Context, _ runtime.ApplyConfiguration, _ ...client.SubResourceApplyOption) error {
	f.parent.subApplied = true
	return nil
}

func TestRoutingClient_Create_PodStatus(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &statusv1beta1.ConstraintTemplatePodStatus{}
	_ = rc.Create(context.Background(), obj)

	if !localCluster.created {
		t.Error("expected Create to route to local cluster for ConstraintTemplatePodStatus")
	}
	if remoteCluster.created {
		t.Error("expected Create NOT to route to remote cluster for ConstraintTemplatePodStatus")
	}
}

func TestRoutingClient_Update_PodStatus(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &statusv1beta1.ConfigPodStatus{}
	_ = rc.Update(context.Background(), obj)

	if !localCluster.updated {
		t.Error("expected Update to route to local cluster for ConfigPodStatus")
	}
	if remoteCluster.updated {
		t.Error("expected Update NOT to route to remote cluster for ConfigPodStatus")
	}
}

func TestRoutingClient_Delete_PodStatus(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &statusv1beta1.MutatorPodStatus{}
	_ = rc.Delete(context.Background(), obj)

	if !localCluster.deleted {
		t.Error("expected Delete to route to local cluster for MutatorPodStatus")
	}
	if remoteCluster.deleted {
		t.Error("expected Delete NOT to route to remote cluster for MutatorPodStatus")
	}
}

func TestRoutingClient_Patch_PodStatus(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &statusv1beta1.ProviderPodStatus{}
	_ = rc.Patch(context.Background(), obj, client.MergeFrom(obj))

	if !localCluster.patched {
		t.Error("expected Patch to route to local cluster for ProviderPodStatus")
	}
	if remoteCluster.patched {
		t.Error("expected Patch NOT to route to remote cluster for ProviderPodStatus")
	}
}

func TestRoutingClient_DeleteAllOf_PodStatus(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &statusv1beta1.ConstraintPodStatus{}
	_ = rc.DeleteAllOf(context.Background(), obj)

	if !localCluster.deletedAllOf {
		t.Error("expected DeleteAllOf to route to local cluster for ConstraintPodStatus")
	}
	if remoteCluster.deletedAllOf {
		t.Error("expected DeleteAllOf NOT to route to remote cluster for ConstraintPodStatus")
	}
}

func TestRoutingClient_DeleteAllOf_NonPodStatus(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels"})
	_ = rc.DeleteAllOf(context.Background(), obj)

	if !remoteCluster.deletedAllOf {
		t.Error("expected DeleteAllOf to route to remote cluster for constraint")
	}
	if localCluster.deletedAllOf {
		t.Error("expected DeleteAllOf NOT to route to local cluster for constraint")
	}
}

func TestRoutingClient_Create_ConnectionPodStatus(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &statusv1alpha1.ConnectionPodStatus{}
	_ = rc.Create(context.Background(), obj)

	if !localCluster.created {
		t.Error("expected Create to route to local cluster for ConnectionPodStatus")
	}
	if remoteCluster.created {
		t.Error("expected Create NOT to route to remote cluster for ConnectionPodStatus")
	}
}

func TestRoutingClient_Create_NonPodStatus(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1beta1", Kind: "ConstraintTemplate"})
	_ = rc.Create(context.Background(), obj)

	if !remoteCluster.created {
		t.Error("expected Create to route to remote cluster for ConstraintTemplate")
	}
	if localCluster.created {
		t.Error("expected Create NOT to route to local cluster for ConstraintTemplate")
	}
}

func TestRoutingClient_Update_NonPodStatus(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels"})
	_ = rc.Update(context.Background(), obj)

	if !remoteCluster.updated {
		t.Error("expected Update to route to remote cluster for constraint")
	}
	if localCluster.updated {
		t.Error("expected Update NOT to route to local cluster for constraint")
	}
}

func TestRoutingClient_NonRemoteMode(t *testing.T) {
	shared := &fakeClient{}
	rc := NewRoutingClient(shared, shared, newScheme())

	obj := &statusv1beta1.ConstraintPodStatus{}
	_ = rc.Create(context.Background(), obj)

	if !shared.created {
		t.Error("expected Create to work in non-remote mode")
	}
}

// errorClient returns errors for testing propagation.
type errorClient struct {
	client.Client
	err     error
	created bool
}

func (e *errorClient) Create(_ context.Context, _ client.Object, _ ...client.CreateOption) error {
	e.created = true
	return e.err
}

func TestRoutingClient_PropagatesErrors(t *testing.T) {
	expectedErr := errors.New("local cluster write error")
	remoteCluster := &fakeClient{}
	localCluster := &errorClient{err: expectedErr}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &statusv1beta1.ExpansionTemplatePodStatus{}
	err := rc.Create(context.Background(), obj)

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if !localCluster.created {
		t.Error("expected Create to route to local cluster for ExpansionTemplatePodStatus")
	}
	if remoteCluster.created {
		t.Error("expected Create NOT to route to remote cluster for ExpansionTemplatePodStatus")
	}
}

func TestRoutingClient_ErrorsOnUnresolvableGVK(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, runtime.NewScheme())

	obj := &unstructured.Unstructured{}
	err := rc.Create(context.Background(), obj)

	if err == nil {
		t.Error("expected an error when GVK cannot be resolved")
	}
	if remoteCluster.created {
		t.Error("expected NOT to route to remote cluster on unresolvable GVK")
	}
	if localCluster.created {
		t.Error("expected NOT to route to local cluster on unresolvable GVK")
	}
}

// SubResource / Status routing tests

func TestRoutingClient_StatusUpdate_PodStatus(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &statusv1beta1.ConfigPodStatus{}
	_ = rc.Status().Update(context.Background(), obj)

	if !localCluster.subUpdated {
		t.Error("expected Status().Update() to route to local cluster for ConfigPodStatus")
	}
	if remoteCluster.subUpdated {
		t.Error("expected Status().Update() NOT to route to remote cluster for ConfigPodStatus")
	}
}

func TestRoutingClient_SubResourcePatch_PodStatus(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &statusv1beta1.ConstraintPodStatus{}
	_ = rc.SubResource("status").Patch(context.Background(), obj, client.Merge)

	if !localCluster.subPatched {
		t.Error("expected SubResource(\"status\").Patch() to route to local cluster for ConstraintPodStatus")
	}
	if remoteCluster.subPatched {
		t.Error("expected SubResource(\"status\").Patch() NOT to route to remote cluster for ConstraintPodStatus")
	}
}

func TestRoutingClient_StatusUpdate_Parent(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels"})
	_ = rc.Status().Update(context.Background(), obj)

	if !remoteCluster.subUpdated {
		t.Error("expected Status().Update() to route to remote cluster for constraint")
	}
	if localCluster.subUpdated {
		t.Error("expected Status().Update() NOT to route to local cluster for constraint")
	}
}

func TestRoutingClient_StatusUpdate_ErrorsOnUnresolvableGVK(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, runtime.NewScheme())

	obj := &unstructured.Unstructured{}
	err := rc.Status().Update(context.Background(), obj)

	if err == nil {
		t.Error("expected an error when GVK cannot be resolved")
	}
	if remoteCluster.subUpdated {
		t.Error("expected NOT to route to remote cluster on unresolvable GVK")
	}
	if localCluster.subUpdated {
		t.Error("expected NOT to route to local cluster on unresolvable GVK")
	}
}

// Reader routing tests

func TestRoutingClient_Get_PodStatus(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &statusv1beta1.ConstraintTemplatePodStatus{}
	_ = rc.Get(context.Background(), client.ObjectKey{Name: "test", Namespace: "gatekeeper-system"}, obj)

	if !localCluster.got {
		t.Error("expected Get to route to local cluster for ConstraintTemplatePodStatus")
	}
	if remoteCluster.got {
		t.Error("expected Get NOT to route to remote cluster for ConstraintTemplatePodStatus")
	}
}

func TestRoutingClient_List_PodStatus(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	list := &statusv1beta1.ConstraintPodStatusList{}
	_ = rc.List(context.Background(), list)

	if !localCluster.listed {
		t.Error("expected List to route to local cluster for ConstraintPodStatusList")
	}
	if remoteCluster.listed {
		t.Error("expected List NOT to route to remote cluster for ConstraintPodStatusList")
	}
}

// fakeApplyConfig is a minimal runtime.ApplyConfiguration that exposes a GVK,
// mimicking an unstructured apply configuration.
type fakeApplyConfig struct {
	gvk schema.GroupVersionKind
}

func (f *fakeApplyConfig) IsApplyConfiguration() {}

func (f *fakeApplyConfig) GetObjectKind() schema.ObjectKind {
	return f
}

func (f *fakeApplyConfig) SetGroupVersionKind(gvk schema.GroupVersionKind) { f.gvk = gvk }

func (f *fakeApplyConfig) GroupVersionKind() schema.GroupVersionKind { return f.gvk }

// typedApplyConfig is an apply configuration that does not expose a GVK,
// mimicking a typed apply configuration.
type typedApplyConfig struct{}

func (t *typedApplyConfig) IsApplyConfiguration() {}

func TestRoutingClient_Apply_PodStatus(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &fakeApplyConfig{gvk: schema.GroupVersionKind{Group: "status.gatekeeper.sh", Version: "v1beta1", Kind: "ConfigPodStatus"}}
	_ = rc.Apply(context.Background(), obj)

	if !localCluster.applied {
		t.Error("expected Apply to route to local cluster for status.gatekeeper.sh apply configuration")
	}
	if remoteCluster.applied {
		t.Error("expected Apply NOT to route to remote cluster for status.gatekeeper.sh apply configuration")
	}
}

func TestRoutingClient_Apply_NonPodStatus(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	obj := &fakeApplyConfig{gvk: schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1beta1", Kind: "ConstraintTemplate"}}
	_ = rc.Apply(context.Background(), obj)

	if !remoteCluster.applied {
		t.Error("expected Apply to route to remote cluster for non-status apply configuration")
	}
	if localCluster.applied {
		t.Error("expected Apply NOT to route to local cluster for non-status apply configuration")
	}
}

func TestRoutingClient_Apply_TypedFallsBackToRemoteCluster(t *testing.T) {
	remoteCluster := &fakeClient{}
	localCluster := &fakeClient{}
	rc := NewRoutingClient(remoteCluster, localCluster, newScheme())

	// Typed apply configs do not expose a GVK, so routing falls back to the remote cluster.
	_ = rc.Apply(context.Background(), &typedApplyConfig{})

	if !remoteCluster.applied {
		t.Error("expected Apply to fall back to remote cluster for typed apply configuration")
	}
	if localCluster.applied {
		t.Error("expected Apply NOT to route to local cluster for typed apply configuration")
	}
}
