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
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// fakeCache is a minimal cache.Cache that records which calls it received.
type fakeCache struct {
	cache.Cache
	name                     string
	getCalled                bool
	listCalled               bool
	getInformerCalled        bool
	getInformerForKindCalled bool
	removeInformerCalled     bool
	indexFieldCalled         bool
}

func (f *fakeCache) Get(_ context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
	f.getCalled = true
	return nil
}

func (f *fakeCache) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	f.listCalled = true
	return nil
}

func (f *fakeCache) GetInformer(_ context.Context, _ client.Object, _ ...cache.InformerGetOption) (cache.Informer, error) {
	f.getInformerCalled = true
	return nil, nil
}

func (f *fakeCache) GetInformerForKind(_ context.Context, _ schema.GroupVersionKind, _ ...cache.InformerGetOption) (cache.Informer, error) {
	f.getInformerForKindCalled = true
	return nil, nil
}

func (f *fakeCache) RemoveInformer(_ context.Context, _ client.Object) error {
	f.removeInformerCalled = true
	return nil
}

func (f *fakeCache) Start(_ context.Context) error {
	return nil
}

func (f *fakeCache) WaitForCacheSync(_ context.Context) bool {
	return true
}

func (f *fakeCache) IndexField(_ context.Context, _ client.Object, _ string, _ client.IndexerFunc) error {
	f.indexFieldCalled = true
	return nil
}

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = statusv1beta1.AddToScheme(s)
	_ = statusv1alpha1.AddToScheme(s)
	return s
}

func TestRoutingCache_Get_TypedPodStatus(t *testing.T) {
	target := &fakeCache{name: "target"}
	mgmt := &fakeCache{name: "management"}
	rc := NewRoutingCache(target, mgmt, newScheme())

	obj := &statusv1beta1.ConstraintTemplatePodStatus{}
	_ = rc.Get(context.Background(), client.ObjectKey{Name: "test", Namespace: "gatekeeper-system"}, obj)

	if !mgmt.getCalled {
		t.Error("expected Get to route to management cache for ConstraintTemplatePodStatus")
	}
	if target.getCalled {
		t.Error("expected Get NOT to route to target cache for ConstraintTemplatePodStatus")
	}
}

func TestRoutingCache_Get_TypedConstraintPodStatus(t *testing.T) {
	target := &fakeCache{name: "target"}
	mgmt := &fakeCache{name: "management"}
	rc := NewRoutingCache(target, mgmt, newScheme())

	obj := &statusv1beta1.ConstraintPodStatus{}
	_ = rc.Get(context.Background(), client.ObjectKey{Name: "test", Namespace: "gatekeeper-system"}, obj)

	if !mgmt.getCalled {
		t.Error("expected Get to route to management cache for ConstraintPodStatus")
	}
	if target.getCalled {
		t.Error("expected Get NOT to route to target cache for ConstraintPodStatus")
	}
}

func TestRoutingCache_Get_ConnectionPodStatus(t *testing.T) {
	target := &fakeCache{name: "target"}
	mgmt := &fakeCache{name: "management"}
	rc := NewRoutingCache(target, mgmt, newScheme())

	obj := &statusv1alpha1.ConnectionPodStatus{}
	_ = rc.Get(context.Background(), client.ObjectKey{Name: "test", Namespace: "gatekeeper-system"}, obj)

	if !mgmt.getCalled {
		t.Error("expected Get to route to management cache for ConnectionPodStatus")
	}
}

func TestRoutingCache_Get_UnstructuredPodStatus(t *testing.T) {
	target := &fakeCache{name: "target"}
	mgmt := &fakeCache{name: "management"}
	rc := NewRoutingCache(target, mgmt, newScheme())

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "status.gatekeeper.sh", Version: "v1beta1", Kind: "ConfigPodStatus"})
	_ = rc.Get(context.Background(), client.ObjectKey{Name: "test", Namespace: "gatekeeper-system"}, obj)

	if !mgmt.getCalled {
		t.Error("expected Get to route to management cache for unstructured PodStatus")
	}
}

func TestRoutingCache_Get_NonPodStatus(t *testing.T) {
	target := &fakeCache{name: "target"}
	mgmt := &fakeCache{name: "management"}
	rc := NewRoutingCache(target, mgmt, newScheme())

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1beta1", Kind: "ConstraintTemplate"})
	_ = rc.Get(context.Background(), client.ObjectKey{Name: "test"}, obj)

	if !target.getCalled {
		t.Error("expected Get to route to target cache for ConstraintTemplate")
	}
	if mgmt.getCalled {
		t.Error("expected Get NOT to route to management cache for ConstraintTemplate")
	}
}

func TestRoutingCache_List_PodStatusList(t *testing.T) {
	target := &fakeCache{name: "target"}
	mgmt := &fakeCache{name: "management"}
	rc := NewRoutingCache(target, mgmt, newScheme())

	list := &statusv1beta1.ConstraintPodStatusList{}
	_ = rc.List(context.Background(), list)

	if !mgmt.listCalled {
		t.Error("expected List to route to management cache for ConstraintPodStatusList")
	}
	if target.listCalled {
		t.Error("expected List NOT to route to target cache for ConstraintPodStatusList")
	}
}

func TestRoutingCache_NonRemoteMode(t *testing.T) {
	// Same pointer for both — simulates non-remote mode.
	shared := &fakeCache{name: "shared"}
	rc := NewRoutingCache(shared, shared, newScheme())

	obj := &statusv1beta1.ConfigPodStatus{}
	_ = rc.Get(context.Background(), client.ObjectKey{Name: "test", Namespace: "gatekeeper-system"}, obj)

	if !shared.getCalled {
		t.Error("expected Get to work in non-remote mode")
	}
}

func TestRoutingCache_WaitForCacheSync_NonRemote(t *testing.T) {
	shared := &fakeCache{name: "shared"}
	rc := NewRoutingCache(shared, shared, newScheme())

	if !rc.WaitForCacheSync(context.Background()) {
		t.Error("expected WaitForCacheSync to return true")
	}
}

func TestRoutingCache_FallbackOnUnresolvableGVK(t *testing.T) {
	target := &fakeCache{name: "target"}
	mgmt := &fakeCache{name: "management"}
	// Use empty scheme where nothing can be resolved.
	rc := NewRoutingCache(target, mgmt, runtime.NewScheme())

	obj := &unstructured.Unstructured{}
	_ = rc.Get(context.Background(), client.ObjectKey{Name: "test"}, obj)

	if !target.getCalled {
		t.Error("expected fallback to target cache when GVK cannot be resolved")
	}
}

// errorCache is a cache.Cache that returns errors, used to verify routing.
type errorCache struct {
	cache.Cache
	err error
}

func (e *errorCache) Get(_ context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
	return e.err
}

func TestRoutingCache_PropagatesErrors(t *testing.T) {
	expectedErr := errors.New("management cache error")
	target := &fakeCache{name: "target"}
	mgmt := &errorCache{err: expectedErr}
	rc := NewRoutingCache(target, mgmt, newScheme())

	obj := &statusv1beta1.MutatorPodStatus{}
	err := rc.Get(context.Background(), client.ObjectKey{Name: "test", Namespace: "ns"}, obj)

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestRoutingCache_GetInformerForKind_PodStatus(t *testing.T) {
	target := &fakeCache{name: "target"}
	mgmt := &fakeCache{name: "management"}
	rc := NewRoutingCache(target, mgmt, newScheme())

	gvk := schema.GroupVersionKind{Group: "status.gatekeeper.sh", Version: "v1beta1", Kind: "ConfigPodStatus"}
	_, _ = rc.GetInformerForKind(context.Background(), gvk)

	if !mgmt.getInformerForKindCalled {
		t.Error("expected GetInformerForKind to route to management for status.gatekeeper.sh GVK")
	}
	if target.getInformerForKindCalled {
		t.Error("expected GetInformerForKind NOT to route to target for status.gatekeeper.sh GVK")
	}
}
