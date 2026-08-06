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
// err is returned by Get and syncResult is returned by WaitForCacheSync.
type fakeCache struct {
	cache.Cache
	err                      error
	syncResult               bool
	getCalled                bool
	listCalled               bool
	getInformerCalled        bool
	getInformerForKindCalled bool
	removeInformerCalled     bool
	indexFieldCalled         bool
}

func (f *fakeCache) Get(_ context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
	f.getCalled = true
	return f.err
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
	return f.syncResult
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
	remoteCluster := &fakeCache{}
	localCluster := &fakeCache{}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	obj := &statusv1beta1.ConstraintTemplatePodStatus{}
	_ = rc.Get(context.Background(), client.ObjectKey{Name: "test", Namespace: "gatekeeper-system"}, obj)

	if !localCluster.getCalled {
		t.Error("expected Get to route to local cluster cache for ConstraintTemplatePodStatus")
	}
	if remoteCluster.getCalled {
		t.Error("expected Get NOT to route to remote cluster cache for ConstraintTemplatePodStatus")
	}
}

func TestRoutingCache_Get_TypedConstraintPodStatus(t *testing.T) {
	remoteCluster := &fakeCache{}
	localCluster := &fakeCache{}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	obj := &statusv1beta1.ConstraintPodStatus{}
	_ = rc.Get(context.Background(), client.ObjectKey{Name: "test", Namespace: "gatekeeper-system"}, obj)

	if !localCluster.getCalled {
		t.Error("expected Get to route to local cluster cache for ConstraintPodStatus")
	}
	if remoteCluster.getCalled {
		t.Error("expected Get NOT to route to remote cluster cache for ConstraintPodStatus")
	}
}

func TestRoutingCache_Get_ConnectionPodStatus(t *testing.T) {
	remoteCluster := &fakeCache{}
	localCluster := &fakeCache{}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	obj := &statusv1alpha1.ConnectionPodStatus{}
	_ = rc.Get(context.Background(), client.ObjectKey{Name: "test", Namespace: "gatekeeper-system"}, obj)

	if !localCluster.getCalled {
		t.Error("expected Get to route to local cluster cache for ConnectionPodStatus")
	}
	if remoteCluster.getCalled {
		t.Error("expected Get NOT to route to remote cluster cache for ConnectionPodStatus")
	}
}

func TestRoutingCache_Get_UnstructuredPodStatus(t *testing.T) {
	remoteCluster := &fakeCache{}
	localCluster := &fakeCache{}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "status.gatekeeper.sh", Version: "v1beta1", Kind: "ConfigPodStatus"})
	_ = rc.Get(context.Background(), client.ObjectKey{Name: "test", Namespace: "gatekeeper-system"}, obj)

	if !localCluster.getCalled {
		t.Error("expected Get to route to local cluster cache for unstructured PodStatus")
	}
	if remoteCluster.getCalled {
		t.Error("expected Get NOT to route to remote cluster cache for unstructured PodStatus")
	}
}

func TestRoutingCache_Get_NonPodStatus(t *testing.T) {
	remoteCluster := &fakeCache{}
	localCluster := &fakeCache{}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1beta1", Kind: "ConstraintTemplate"})
	_ = rc.Get(context.Background(), client.ObjectKey{Name: "test"}, obj)

	if !remoteCluster.getCalled {
		t.Error("expected Get to route to remote cluster cache for ConstraintTemplate")
	}
	if localCluster.getCalled {
		t.Error("expected Get NOT to route to local cluster cache for ConstraintTemplate")
	}
}

func TestRoutingCache_List_PodStatusList(t *testing.T) {
	remoteCluster := &fakeCache{}
	localCluster := &fakeCache{}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	list := &statusv1beta1.ConstraintPodStatusList{}
	_ = rc.List(context.Background(), list)

	if !localCluster.listCalled {
		t.Error("expected List to route to local cluster cache for ConstraintPodStatusList")
	}
	if remoteCluster.listCalled {
		t.Error("expected List NOT to route to remote cluster cache for ConstraintPodStatusList")
	}
}

func TestRoutingCache_NonRemoteMode(t *testing.T) {
	// Same pointer for both — simulates non-remote mode.
	shared := &fakeCache{}
	rc := NewRoutingCache(shared, shared, newScheme())

	obj := &statusv1beta1.ConfigPodStatus{}
	_ = rc.Get(context.Background(), client.ObjectKey{Name: "test", Namespace: "gatekeeper-system"}, obj)

	if !shared.getCalled {
		t.Error("expected Get to work in non-remote mode")
	}
}

func TestRoutingCache_WaitForCacheSync_NonRemote(t *testing.T) {
	shared := &fakeCache{syncResult: true}
	rc := NewRoutingCache(shared, shared, newScheme())

	if !rc.WaitForCacheSync(context.Background()) {
		t.Error("expected WaitForCacheSync to return true")
	}
}

func TestRoutingCache_ErrorsOnUnresolvableGVK(t *testing.T) {
	remoteCluster := &fakeCache{}
	localCluster := &fakeCache{}
	// Use empty scheme where nothing can be resolved.
	rc := NewRoutingCache(remoteCluster, localCluster, runtime.NewScheme())

	obj := &unstructured.Unstructured{}
	err := rc.Get(context.Background(), client.ObjectKey{Name: "test"}, obj)

	if err == nil {
		t.Error("expected an error when GVK cannot be resolved")
	}
	if remoteCluster.getCalled {
		t.Error("expected NOT to route to remote cluster cache when GVK cannot be resolved")
	}
	if localCluster.getCalled {
		t.Error("expected NOT to route to local cluster cache when GVK cannot be resolved")
	}
}

func TestRoutingCache_PropagatesErrors(t *testing.T) {
	expectedErr := errors.New("local cluster cache error")
	remoteCluster := &fakeCache{}
	localCluster := &fakeCache{err: expectedErr}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	obj := &statusv1beta1.MutatorPodStatus{}
	err := rc.Get(context.Background(), client.ObjectKey{Name: "test", Namespace: "ns"}, obj)

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if !localCluster.getCalled {
		t.Error("expected Get to route to local cluster cache for MutatorPodStatus")
	}
	if remoteCluster.getCalled {
		t.Error("expected Get NOT to route to remote cluster cache for MutatorPodStatus")
	}
}

func TestRoutingCache_GetInformerForKind_PodStatus(t *testing.T) {
	remoteCluster := &fakeCache{}
	localCluster := &fakeCache{}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	gvk := schema.GroupVersionKind{Group: "status.gatekeeper.sh", Version: "v1beta1", Kind: "ConfigPodStatus"}
	_, _ = rc.GetInformerForKind(context.Background(), gvk)

	if !localCluster.getInformerForKindCalled {
		t.Error("expected GetInformerForKind to route to local cluster for status.gatekeeper.sh GVK")
	}
	if remoteCluster.getInformerForKindCalled {
		t.Error("expected GetInformerForKind NOT to route to remote cluster for status.gatekeeper.sh GVK")
	}
}

func TestRoutingCache_GetInformer_PodStatus(t *testing.T) {
	remoteCluster := &fakeCache{}
	localCluster := &fakeCache{}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	obj := &statusv1beta1.ConstraintTemplatePodStatus{}
	_, _ = rc.GetInformer(context.Background(), obj)

	if !localCluster.getInformerCalled {
		t.Error("expected GetInformer to route to local cluster cache for ConstraintTemplatePodStatus")
	}
	if remoteCluster.getInformerCalled {
		t.Error("expected GetInformer NOT to route to remote cluster cache for ConstraintTemplatePodStatus")
	}
}

func TestRoutingCache_GetInformer_NonPodStatus(t *testing.T) {
	remoteCluster := &fakeCache{}
	localCluster := &fakeCache{}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1beta1", Kind: "ConstraintTemplate"})
	_, _ = rc.GetInformer(context.Background(), obj)

	if !remoteCluster.getInformerCalled {
		t.Error("expected GetInformer to route to remote cluster cache for ConstraintTemplate")
	}
	if localCluster.getInformerCalled {
		t.Error("expected GetInformer NOT to route to local cluster cache for ConstraintTemplate")
	}
}

func TestRoutingCache_RemoveInformer_PodStatus(t *testing.T) {
	remoteCluster := &fakeCache{}
	localCluster := &fakeCache{}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	obj := &statusv1beta1.ConfigPodStatus{}
	_ = rc.RemoveInformer(context.Background(), obj)

	if !localCluster.removeInformerCalled {
		t.Error("expected RemoveInformer to route to local cluster cache for ConfigPodStatus")
	}
	if remoteCluster.removeInformerCalled {
		t.Error("expected RemoveInformer NOT to route to remote cluster cache for ConfigPodStatus")
	}
}

func TestRoutingCache_RemoveInformer_NonPodStatus(t *testing.T) {
	remoteCluster := &fakeCache{}
	localCluster := &fakeCache{}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels"})
	_ = rc.RemoveInformer(context.Background(), obj)

	if !remoteCluster.removeInformerCalled {
		t.Error("expected RemoveInformer to route to remote cluster cache for constraint")
	}
	if localCluster.removeInformerCalled {
		t.Error("expected RemoveInformer NOT to route to local cluster cache for constraint")
	}
}

func TestRoutingCache_IndexField_PodStatus(t *testing.T) {
	remoteCluster := &fakeCache{}
	localCluster := &fakeCache{}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	obj := &statusv1beta1.MutatorPodStatus{}
	_ = rc.IndexField(context.Background(), obj, "field", func(client.Object) []string { return nil })

	if !localCluster.indexFieldCalled {
		t.Error("expected IndexField to route to local cluster cache for MutatorPodStatus")
	}
	if remoteCluster.indexFieldCalled {
		t.Error("expected IndexField NOT to route to remote cluster cache for MutatorPodStatus")
	}
}

func TestRoutingCache_IndexField_NonPodStatus(t *testing.T) {
	remoteCluster := &fakeCache{}
	localCluster := &fakeCache{}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "templates.gatekeeper.sh", Version: "v1beta1", Kind: "ConstraintTemplate"})
	_ = rc.IndexField(context.Background(), obj, "field", func(client.Object) []string { return nil })

	if !remoteCluster.indexFieldCalled {
		t.Error("expected IndexField to route to remote cluster cache for ConstraintTemplate")
	}
	if localCluster.indexFieldCalled {
		t.Error("expected IndexField NOT to route to local cluster cache for ConstraintTemplate")
	}
}

func TestRoutingCache_WaitForCacheSync_Remote(t *testing.T) {
	tests := []struct {
		name       string
		remoteSync bool
		localSync  bool
		want       bool
	}{
		{name: "both synced", remoteSync: true, localSync: true, want: true},
		{name: "remote cluster not synced", remoteSync: false, localSync: true, want: false},
		{name: "local cluster not synced", remoteSync: true, localSync: false, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remoteCluster := &fakeCache{syncResult: tt.remoteSync}
			localCluster := &fakeCache{syncResult: tt.localSync}
			rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

			if got := rc.WaitForCacheSync(context.Background()); got != tt.want {
				t.Errorf("WaitForCacheSync() = %v, want %v", got, tt.want)
			}
		})
	}
}

// blockingCache blocks in Start() until context is canceled, or returns startErr immediately.
type blockingCache struct {
	cache.Cache
	startErr error
}

func (b *blockingCache) Start(ctx context.Context) error {
	if b.startErr != nil {
		return b.startErr
	}
	<-ctx.Done()
	return nil
}

func (b *blockingCache) WaitForCacheSync(_ context.Context) bool {
	return true
}

func TestRoutingCache_Start_LocalClusterFailure(t *testing.T) {
	localErr := errors.New("connection refused")
	remoteCluster := &blockingCache{}
	localCluster := &blockingCache{startErr: localErr}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	err := rc.Start(context.Background())
	if err == nil {
		t.Fatal("expected Start to return an error when local cluster cache fails")
	}
	if !errors.Is(err, localErr) {
		t.Errorf("expected wrapped local cluster error, got: %v", err)
	}
}

func TestRoutingCache_Start_NormalShutdown(t *testing.T) {
	remoteCluster := &blockingCache{}
	localCluster := &blockingCache{}
	rc := NewRoutingCache(remoteCluster, localCluster, newScheme())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := rc.Start(ctx)
	if err != nil {
		t.Errorf("expected nil error on normal shutdown, got: %v", err)
	}
}
