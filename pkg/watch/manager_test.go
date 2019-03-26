package watch

import (
	"context"
	"fmt"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
)

func newForTest(c client.Reader) *WatchManager {
	wm := &WatchManager{
		client:       c,
		newMgrFn:     newFakeMgr,
		stopper:      make(chan struct{}),
		managedKinds: newRecordKeeper(),
		watchedKinds: make(map[string]watchVitals),
		cfg:          nil,
	}
	wm.managedKinds.mgr = wm
	return wm
}

func newFakeMgr(wm *WatchManager) (manager.Manager, error) {
	return &fakeMgr{}, nil
}

var _ manager.Manager = &fakeMgr{}

type fakeMgr struct{}

func (m *fakeMgr) Add(runnable manager.Runnable) error {
	return nil
}

func (m *fakeMgr) SetFields(interface{}) error {
	return nil
}

func (m *fakeMgr) Start(c <-chan struct{}) error {
	<-c
	return nil
}

func (m *fakeMgr) GetConfig() *rest.Config {
	return nil
}

func (m *fakeMgr) GetScheme() *runtime.Scheme {
	return nil
}

func (m *fakeMgr) GetAdmissionDecoder() types.Decoder {
	return nil
}

func (m *fakeMgr) GetClient() client.Client {
	return nil
}

func (m *fakeMgr) GetFieldIndexer() client.FieldIndexer {
	return nil
}

func (m *fakeMgr) GetCache() cache.Cache {
	return nil
}

func (m *fakeMgr) GetRecorder(name string) record.EventRecorder {
	return nil
}

func (m *fakeMgr) GetRESTMapper() meta.RESTMapper {
	return nil
}

var _ client.Reader = &fakeClient{}

type fakeClient struct {
	notFound bool
}

func (c *fakeClient) Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
	if !c.notFound {
		crd, ok := obj.(*apiextensionsv1beta1.CustomResourceDefinition)
		if !ok {
			panic(fmt.Sprintf("CRD not passed to client: %+v", obj))
		}
		// This means the CRD kind and name must be equal
		crd.Status.AcceptedNames = apiextensionsv1beta1.CustomResourceDefinitionNames{Kind: key.Name}
		crd.ObjectMeta.ResourceVersion = "versioningisfun"
		return nil
	}
	return errors.NewNotFound(schema.GroupResource{Group: "", Resource: "customresourcedefinition"}, key.Name)
}

func (c *fakeClient) List(ctx context.Context, opts *client.ListOptions, list runtime.Object) error {
	return nil
}

func newChange(kind, version string, r ...*Registrar) map[string]watchVitals {
	rs := make(map[*Registrar]bool)
	for _, v := range r {
		rs[v] = true
	}
	return map[string]watchVitals{kind: {kind: kind, crdName: kind, version: version, registrars: rs}}
}

func TestRegistrar(t *testing.T) {
	wm := newForTest(&fakeClient{})
	defer wm.Close()
	reg, err := wm.NewRegistrar("foo", nil)
	if err != nil {
		t.Fatalf("Error setting up registrar: %s", err)
	}
	if err := reg.AddWatch("FooCRD", "FooCRD"); err != nil {
		t.Fatalf("Error adding watch: %s", err)
	}

	t.Run("Single Add Watch", func(t *testing.T) {
		expectedAdded := newChange("FooCRD", "", reg)
		added, removed, changed, err := wm.gatherChanges(wm.managedKinds.Get())
		if diff := cmp.Diff(added, expectedAdded, cmp.AllowUnexported(watchVitals{})); diff != "" {
			t.Error(diff)
		}
		if len(removed) != 0 {
			t.Errorf("removed = %s, wanted empty map", spew.Sdump(removed))
		}
		if len(changed) != 0 {
			t.Errorf("changed = %s, wanted empty map", spew.Sdump(changed))
		}
		if err != nil {
			t.Errorf("err = %s, want nil", err)
		}
		b, err := wm.updateManager()
		if err != nil {
			t.Errorf("Could not update manager: %s", err)
		}
		if b == false {
			t.Errorf("Manager not restarted on first add")
		}
	})

	t.Run("Second add watch does nothing", func(t *testing.T) {
		if err := reg.AddWatch("FooCRD", "FooCRD"); err != nil {
			t.Fatalf("Error adding second watch: %s", err)
		}
		added, removed, changed, err := wm.gatherChanges(wm.managedKinds.Get())
		if len(added) != 0 {
			t.Errorf("added = %s, wanted empty map", spew.Sdump(added))
		}
		if len(removed) != 0 {
			t.Errorf("removed = %s, wanted empty map", spew.Sdump(removed))
		}
		if len(changed) != 0 {
			t.Errorf("changed = %s, wanted empty map", spew.Sdump(changed))
		}
		if err != nil {
			t.Errorf("err = %s, want nil", err)
		}
		b, err := wm.updateManager()
		if err != nil {
			t.Errorf("Could not update manager: %s", err)
		}
		if b == true {
			t.Errorf("Manager restarted, wanted no op")
		}
	})

	reg2, err := wm.NewRegistrar("bar", nil)
	if err != nil {
		t.Fatalf("Error setting up 2nd registrar: %s", err)
	}
	t.Run("New registrar makes for a restart", func(t *testing.T) {
		if err := reg2.AddWatch("FooCRD", "FooCRD"); err != nil {
			t.Fatalf("Error adding watch: %s", err)
		}
		expectedChanged := newChange("FooCRD", "versioningisfun", reg, reg2)
		added, removed, changed, err := wm.gatherChanges(wm.managedKinds.Get())
		if len(added) != 0 {
			t.Errorf("added = %s, wanted empty map", spew.Sdump(added))
		}
		if len(removed) != 0 {
			t.Errorf("removed = %s, wanted empty map", spew.Sdump(removed))
		}
		if diff := cmp.Diff(changed, expectedChanged, cmp.AllowUnexported(watchVitals{})); diff != "" {
			t.Error(diff)
		}
		if err != nil {
			t.Errorf("err = %s, want nil", err)
		}
		b, err := wm.updateManager()
		if err != nil {
			t.Errorf("Could not update manager: %s", err)
		}
		if b == false {
			t.Errorf("Manager not restarted")
		}
	})

	t.Run("First remove makes for a change", func(t *testing.T) {
		if err := reg2.RemoveWatch("FooCRD"); err != nil {
			t.Fatalf("Error removing watch: %s", err)
		}
		expectedChanged := newChange("FooCRD", "versioningisfun", reg)
		added, removed, changed, err := wm.gatherChanges(wm.managedKinds.Get())
		if len(added) != 0 {
			t.Errorf("added = %s, wanted empty map", spew.Sdump(added))
		}
		if len(removed) != 0 {
			t.Errorf("removed = %s, wanted empty map", spew.Sdump(removed))
		}
		if diff := cmp.Diff(changed, expectedChanged, cmp.AllowUnexported(watchVitals{})); diff != "" {
			t.Error(diff)
		}
		if err != nil {
			t.Errorf("err = %s, want nil", err)
		}
		b, err := wm.updateManager()
		if err != nil {
			t.Errorf("Could not update manager: %s", err)
		}
		if b == false {
			t.Errorf("Manager not restarted")
		}
	})

	t.Run("Second remove makes for a remove", func(t *testing.T) {
		if err := reg.RemoveWatch("FooCRD"); err != nil {
			t.Fatalf("Error removing watch: %s", err)
		}
		expectedRemoved := newChange("FooCRD", "versioningisfun", reg)
		added, removed, changed, err := wm.gatherChanges(wm.managedKinds.Get())
		if len(added) != 0 {
			t.Errorf("added = %s, wanted empty map", spew.Sdump(added))
		}
		if diff := cmp.Diff(removed, expectedRemoved, cmp.AllowUnexported(watchVitals{})); diff != "" {
			t.Error(diff)
		}
		if len(changed) != 0 {
			t.Errorf("changed = %s, wanted empty map", spew.Sdump(removed))
		}
		if err != nil {
			t.Errorf("err = %s, want nil", err)
		}
		b, err := wm.updateManager()
		if err != nil {
			t.Errorf("Could not update manager: %s", err)
		}
		if b == false {
			t.Errorf("Manager not restarted")
		}
	})

	if err := reg.AddWatch("FooCRD", "FooCRD"); err != nil {
		t.Fatalf("Error adding watch: %s", err)
	}
	t.Run("Single Add Waits For CRD Available", func(t *testing.T) {
		wm.client = &fakeClient{notFound: true}
		expectedAdded := newChange("FooCRD", "", reg)
		added, removed, changed, err := wm.gatherChanges(wm.managedKinds.Get())
		if diff := cmp.Diff(added, expectedAdded, cmp.AllowUnexported(watchVitals{})); diff != "" {
			t.Error(diff)
		}
		if len(removed) != 0 {
			t.Errorf("removed = %s, wanted empty map", spew.Sdump(removed))
		}
		if len(changed) != 0 {
			t.Errorf("changed = %s, wanted empty map", spew.Sdump(changed))
		}
		if err != nil {
			t.Errorf("err = %s, want nil", err)
		}
		b, err := wm.updateManager()
		if err != nil {
			t.Errorf("Could not update manager: %s", err)
		}
		if b == true {
			t.Errorf("Manager should not have restarted while CRD is pending")
		}

		wm.client = &fakeClient{}
		b, err = wm.updateManager()
		if err != nil {
			t.Errorf("Could not update manager: %s", err)
		}
		if b == false {
			t.Errorf("Manager should have updated now that CRD is found")
		}
	})
}
