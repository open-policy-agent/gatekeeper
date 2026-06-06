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

func TestRoutingClient_Create_PodStatus(t *testing.T) {
	target := &fakeClient{}
	mgmt := &fakeClient{}
	rc := NewRoutingClient(target, mgmt, newScheme())

	obj := &statusv1beta1.ConstraintTemplatePodStatus{}
	_ = rc.Create(context.Background(), obj)

	if !mgmt.created {
		t.Error("expected Create to route to management for ConstraintTemplatePodStatus")
	}
	if target.created {
		t.Error("expected Create NOT to route to target for ConstraintTemplatePodStatus")
	}
}

func TestRoutingClient_Update_PodStatus(t *testing.T) {
	target := &fakeClient{}
	mgmt := &fakeClient{}
	rc := NewRoutingClient(target, mgmt, newScheme())

	obj := &statusv1beta1.ConfigPodStatus{}
	_ = rc.Update(context.Background(), obj)

	if !mgmt.updated {
		t.Error("expected Update to route to management for ConfigPodStatus")
	}
	if target.updated {
		t.Error("expected Update NOT to route to target for ConfigPodStatus")
	}
}

func TestRoutingClient_Delete_PodStatus(t *testing.T) {
	target := &fakeClient{}
	mgmt := &fakeClient{}
	rc := NewRoutingClient(target, mgmt, newScheme())

	obj := &statusv1beta1.MutatorPodStatus{}
	_ = rc.Delete(context.Background(), obj)

	if !mgmt.deleted {
		t.Error("expected Delete to route to management for MutatorPodStatus")
	}
	if target.deleted {
		t.Error("expected Delete NOT to route to target for MutatorPodStatus")
	}
}

func TestRoutingClient_Patch_PodStatus(t *testing.T) {
	target := &fakeClient{}
	mgmt := &fakeClient{}
	rc := NewRoutingClient(target, mgmt, newScheme())

	obj := &statusv1beta1.ProviderPodStatus{}
	_ = rc.Patch(context.Background(), obj, client.MergeFrom(obj))

	if !mgmt.patched {
		t.Error("expected Patch to route to management for ProviderPodStatus")
	}
	if target.patched {
		t.Error("expected Patch NOT to route to target for ProviderPodStatus")
	}
}

func TestRoutingClient_DeleteAllOf_PodStatus(t *testing.T) {
	target := &fakeClient{}
	mgmt := &fakeClient{}
	rc := NewRoutingClient(target, mgmt, newScheme())

	obj := &statusv1beta1.ConstraintPodStatus{}
	_ = rc.DeleteAllOf(context.Background(), obj)

	if !mgmt.deletedAllOf {
		t.Error("expected DeleteAllOf to route to management for ConstraintPodStatus")
	}
	if target.deletedAllOf {
		t.Error("expected DeleteAllOf NOT to route to target for ConstraintPodStatus")
	}
}

func TestRoutingClient_DeleteAllOf_NonPodStatus(t *testing.T) {
	target := &fakeClient{}
	mgmt := &fakeClient{}
	rc := NewRoutingClient(target, mgmt, newScheme())

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels"})
	_ = rc.DeleteAllOf(context.Background(), obj)

	if !target.deletedAllOf {
		t.Error("expected DeleteAllOf to route to target for constraint")
	}
	if mgmt.deletedAllOf {
		t.Error("expected DeleteAllOf NOT to route to management for constraint")
	}
}

func TestRoutingClient_Create_ConnectionPodStatus(t *testing.T) {
	target := &fakeClient{}
	mgmt := &fakeClient{}
	rc := NewRoutingClient(target, mgmt, newScheme())

	obj := &statusv1alpha1.ConnectionPodStatus{}
	_ = rc.Create(context.Background(), obj)

	if !mgmt.created {
		t.Error("expected Create to route to management for ConnectionPodStatus")
	}
	if target.created {
		t.Error("expected Create NOT to route to target for ConnectionPodStatus")
	}
}

func TestRoutingClient_Create_NonPodStatus(t *testing.T) {
	target := &fakeClient{}
	mgmt := &fakeClient{}
	rc := NewRoutingClient(target, mgmt, newScheme())

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1beta1", Kind: "ConstraintTemplate"})
	_ = rc.Create(context.Background(), obj)

	if !target.created {
		t.Error("expected Create to route to target for ConstraintTemplate")
	}
	if mgmt.created {
		t.Error("expected Create NOT to route to management for ConstraintTemplate")
	}
}

func TestRoutingClient_Update_NonPodStatus(t *testing.T) {
	target := &fakeClient{}
	mgmt := &fakeClient{}
	rc := NewRoutingClient(target, mgmt, newScheme())

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels"})
	_ = rc.Update(context.Background(), obj)

	if !target.updated {
		t.Error("expected Update to route to target for constraint")
	}
	if mgmt.updated {
		t.Error("expected Update NOT to route to management for constraint")
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
	expectedErr := errors.New("management write error")
	target := &fakeClient{}
	mgmt := &errorClient{err: expectedErr}
	rc := NewRoutingClient(target, mgmt, newScheme())

	obj := &statusv1beta1.ExpansionTemplatePodStatus{}
	err := rc.Create(context.Background(), obj)

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if !mgmt.created {
		t.Error("expected Create to route to management for ExpansionTemplatePodStatus")
	}
	if target.created {
		t.Error("expected Create NOT to route to target for ExpansionTemplatePodStatus")
	}
}

func TestRoutingClient_FallbackOnUnresolvableGVK(t *testing.T) {
	target := &fakeClient{}
	mgmt := &fakeClient{}
	rc := NewRoutingClient(target, mgmt, runtime.NewScheme())

	obj := &unstructured.Unstructured{}
	_ = rc.Create(context.Background(), obj)

	if !target.created {
		t.Error("expected fallback to target on unresolvable GVK")
	}
	if mgmt.created {
		t.Error("expected fallback NOT to route to management on unresolvable GVK")
	}
}

// Reader routing tests

func TestRoutingClient_Get_PodStatus(t *testing.T) {
	target := &fakeClient{}
	mgmt := &fakeClient{}
	rc := NewRoutingClient(target, mgmt, newScheme())

	obj := &statusv1beta1.ConstraintTemplatePodStatus{}
	_ = rc.Get(context.Background(), client.ObjectKey{Name: "test", Namespace: "gatekeeper-system"}, obj)

	if !mgmt.got {
		t.Error("expected Get to route to management for ConstraintTemplatePodStatus")
	}
	if target.got {
		t.Error("expected Get NOT to route to target for ConstraintTemplatePodStatus")
	}
}

func TestRoutingClient_List_PodStatus(t *testing.T) {
	target := &fakeClient{}
	mgmt := &fakeClient{}
	rc := NewRoutingClient(target, mgmt, newScheme())

	list := &statusv1beta1.ConstraintPodStatusList{}
	_ = rc.List(context.Background(), list)

	if !mgmt.listed {
		t.Error("expected List to route to management for ConstraintPodStatusList")
	}
	if target.listed {
		t.Error("expected List NOT to route to target for ConstraintPodStatusList")
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
	target := &fakeClient{}
	mgmt := &fakeClient{}
	rc := NewRoutingClient(target, mgmt, newScheme())

	obj := &fakeApplyConfig{gvk: schema.GroupVersionKind{Group: "status.gatekeeper.sh", Version: "v1beta1", Kind: "ConfigPodStatus"}}
	_ = rc.Apply(context.Background(), obj)

	if !mgmt.applied {
		t.Error("expected Apply to route to management for status.gatekeeper.sh apply configuration")
	}
	if target.applied {
		t.Error("expected Apply NOT to route to target for status.gatekeeper.sh apply configuration")
	}
}

func TestRoutingClient_Apply_NonPodStatus(t *testing.T) {
	target := &fakeClient{}
	mgmt := &fakeClient{}
	rc := NewRoutingClient(target, mgmt, newScheme())

	obj := &fakeApplyConfig{gvk: schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1beta1", Kind: "ConstraintTemplate"}}
	_ = rc.Apply(context.Background(), obj)

	if !target.applied {
		t.Error("expected Apply to route to target for non-status apply configuration")
	}
	if mgmt.applied {
		t.Error("expected Apply NOT to route to management for non-status apply configuration")
	}
}

func TestRoutingClient_Apply_TypedFallsBackToTarget(t *testing.T) {
	target := &fakeClient{}
	mgmt := &fakeClient{}
	rc := NewRoutingClient(target, mgmt, newScheme())

	// Typed apply configs do not expose a GVK, so routing falls back to target.
	_ = rc.Apply(context.Background(), &typedApplyConfig{})

	if !target.applied {
		t.Error("expected Apply to fall back to target for typed apply configuration")
	}
	if mgmt.applied {
		t.Error("expected Apply NOT to route to management for typed apply configuration")
	}
}
