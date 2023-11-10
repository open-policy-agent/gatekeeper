/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package watch_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// setupManager sets up a controller-runtime manager with registered watch manager.
func setupManager(t *testing.T) (manager.Manager, *watch.Manager) {
	t.Helper()

	metrics.Registry = prometheus.NewRegistry()
	mgr, err := manager.New(cfg, manager.Options{
		MetricsBindAddress: "0",
		MapperProvider:     apiutil.NewDynamicRESTMapper,
		Logger:             testutils.NewLogger(t),
	})
	if err != nil {
		t.Fatalf("setting up controller manager: %s", err)
	}
	c := mgr.GetCache()
	dc, ok := c.(watch.RemovableCache)
	if !ok {
		t.Fatalf("expected dynamic cache, got: %T", c)
	}
	wm, err := watch.New(dc)
	if err != nil {
		t.Fatalf("could not create watch manager: %s", err)
	}
	if err := mgr.Add(wm); err != nil {
		t.Fatalf("could not add watch manager to manager: %s", err)
	}
	return mgr, wm
}

func setupController(mgr manager.Manager, r reconcile.Reconciler, events chan event.GenericEvent) error {
	// Create a new controller
	c, err := controller.New("test-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return fmt.Errorf("creating controller: %w", err)
	}

	// Watch for changes to the provided constraint
	return c.Watch(
		&source.Channel{
			Source:         events,
			DestBufferSize: 1024,
		},
		handler.EnqueueRequestsFromMapFunc(util.EventPackerMapFunc()),
	)
}

// Verify that an unknown resource will return an error when adding a watch.
func TestRegistrar_AddUnknown(t *testing.T) {
	mgr, wm := setupManager(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	grp, ctx := errgroup.WithContext(ctx)

	grp.Go(func() error {
		return mgr.Start(ctx)
	})

	events := make(chan event.GenericEvent)
	r, err := wm.NewRegistrar("test", events)
	if err != nil {
		t.Fatalf("creating registrar: %v", err)
	}

	err = r.AddWatch(ctx, schema.GroupVersionKind{
		Group:   "i",
		Version: "donot",
		Kind:    "exist",
	})
	if err == nil {
		t.Fatalf("AddWatch should have failed due to unknown GVK")
	}

	cancel()
	_ = grp.Wait()
}

// Verifies that controller-runtime interleaves reconcile errors in backoff and
// other events within the same work queue.
func Test_ReconcileErrorDoesNotBlockController(t *testing.T) {
	mgr, _ := setupManager(t)
	ctrl.SetLogger(logr.New(logf.NullLogSink{}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	grp, ctx := errgroup.WithContext(ctx)

	grp.Go(func() error {
		return mgr.Start(ctx)
	})

	// Events will be used to receive events from dynamic watches registered
	// via the registrar below.
	errObj := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("error"),
	)

	events := make(chan event.GenericEvent, 1024)
	events <- event.GenericEvent{
		Object: errObj,
	}

	requests := make(chan reconcile.Request)
	rec := func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
		select {
		case requests <- request:
		case <-ctx.Done():
		}

		if request.Name == "error" {
			return reconcile.Result{}, errors.New("synthetic error")
		}

		return reconcile.Result{}, nil
	}

	// Create a new controller
	c, err := controller.New("test-controller", mgr, controller.Options{Reconciler: reconcile.Func(rec)})
	if err != nil {
		t.Fatalf("creating controller: %v", err)
	}
	err = c.Watch(
		&source.Channel{
			Source:         events,
			DestBufferSize: 1024,
		},
		&handler.EnqueueRequestForObject{},
	)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the error resource to reconcile
	// before setting up another watch.
	<-requests

	// Setup another watch. Show that both the error resource (in a backoff-requeue loop)
	// and other resources can reconcile in an interleaving fashion.
	err = c.Watch(
		source.Kind(mgr.GetCache(), &corev1.Namespace{}),
		&handler.EnqueueRequestForObject{},
	)
	if err != nil {
		t.Fatal(err)
	}

	expectNames := map[string]bool{"error": true, "default": true, "kube-system": true}
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case req := <-requests:
			if expectNames[req.Name] {
				delete(expectNames, req.Name)
			}
			if len(expectNames) == 0 {
				// Test successful
				break loop
			}
		}
	}

	if len(expectNames) > 0 {
		t.Errorf("did not see expected resources: %v", expectNames)
	}
	cancel()
	_ = grp.Wait()
}

// Verifies that a dynamic watch will deliver events even across de-registration and re-registration of a watched CRD.
func TestRegistrar_Reconnect(t *testing.T) {
	mgr, wm := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	grp, ctx := errgroup.WithContext(ctx)

	grp.Go(func() error {
		return mgr.Start(ctx)
	})

	events := make(chan event.GenericEvent)
	r, err := wm.NewRegistrar("test", events)
	if err != nil {
		t.Fatalf("creating registrar: %v", err)
	}

	req := make(chan reconcile.Request)
	rec := reconcile.Func(func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
		select {
		case req <- request:
		case <-ctx.Done():
		}
		return reconcile.Result{}, nil
	})
	err = setupController(mgr, rec, events)
	if err != nil {
		t.Fatalf("creating controller: %v", err)
	}

	gvk := schema.GroupVersionKind{
		Group:   "com.tests",
		Version: "v1alpha1",
		Kind:    "TestResource",
	}
	const plural = "testresources"
	crd := makeCRD(gvk, plural)
	err = applyCRD(ctx, t, c, gvk, crd)
	if err != nil {
		t.Fatalf("applying CRD: %v", err)
	}

	err = r.AddWatch(ctx, gvk)
	if err != nil {
		t.Fatalf("adding watch: %v", err)
	}

	// Create watched resources
	u := unstructuredFor(gvk, "test-add")
	err = c.Create(ctx, u)
	if err != nil {
		t.Fatalf("creating watched resource: %v", err)
	}

	// Wait for create event
	<-req

	// Delete the CRD with an active watch still enabled
	err = c.Delete(ctx, crd)
	if err != nil {
		t.Fatalf("deleting CRD: %v", err)
	}

	// We'll get a delete event for the resource (cascade delete from the parent CRD). Consume it.
	<-req

	// Verify the CRD is gone
	err = waitForCRDToUnregister(ctx, mgr.GetConfig(), gvk, plural)
	if err != nil {
		t.Fatalf("waiting for CRD to unregister: %v", err)
	}

	// Create the CRD and resource again, expect our previous watch to pick them up automatically.
	crd = makeCRD(gvk, plural)
	err = applyCRD(ctx, t, c, gvk, crd)
	if err != nil {
		t.Fatalf("reapplying CRD: %v", err)
	}
	u = unstructuredFor(gvk, "test-add-again")
	err = c.Create(ctx, u)
	if err != nil {
		t.Fatalf("recreating watched resource: %v", err)
	}

	// Wait for create event picked, up by our previous watch.
	result := <-req
	wantSubstr := "test-add-again"
	if !strings.Contains(result.Name, wantSubstr) {
		t.Fatalf("got result.Name = %q but want to contain %q", result, wantSubstr)
	}

	cancel()
	_ = grp.Wait()
}

// Verifies joined watches receive replayed events.
func Test_Registrar_Replay(t *testing.T) {
	mgr, wm := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	grp, ctx := errgroup.WithContext(ctx)

	setupController := func(name string, gvk schema.GroupVersionKind) <-chan reconcile.Request {
		// Events will be used to receive events from dynamic watches registered
		// via the registrar below.
		events := make(chan event.GenericEvent, 1024)

		r, err := wm.NewRegistrar(name, events)
		if err != nil {
			t.Fatalf("creating registrar: %v", err)
		}

		requests := make(chan reconcile.Request)
		rec := func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
			select {
			case requests <- request:
			case <-ctx.Done():
			}

			return reconcile.Result{}, nil
		}

		// Create a new controller
		c, err := controller.New(name, mgr, controller.Options{Reconciler: reconcile.Func(rec)})
		if err != nil {
			t.Fatalf("creating controller: %v", err)
		}
		err = c.Watch(
			&source.Channel{
				Source:         events,
				DestBufferSize: 1024,
			},
			&handler.EnqueueRequestForObject{},
		)
		if err != nil {
			t.Fatal(err)
		}

		err = r.AddWatch(ctx, gvk)
		if err != nil {
			t.Fatal(err)
		}

		return requests
	}

	grp.Go(func() error {
		return mgr.Start(ctx)
	})

	gvk := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	}
	const namespace = "default"
	fixtures := withNamespace(unstructuredList(gvk, "replay-test", 5), namespace)

	c1 := setupController("test-controller-1", gvk)

	// Create some resources
	for _, obj := range fixtures {
		if ctx.Err() != nil {
			t.Fatalf("timout while creating fixtures")
		}
		err := c.Create(ctx, obj)
		if err != nil {
			t.Fatalf("creating fixture: %s", obj.GetName())
		}
	}

	// These should be received via watch events
	if err := waitForExpected(ctx, fixtures, c1, namespace); err != nil {
		t.Fatalf("waiting for live watches: %v", err)
	}

	// Setup a second watcher, it should receive the same events replayed instead of live:
	c2 := setupController("test-controller-2", gvk)
	if err := waitForExpected(ctx, fixtures, c2, namespace); err != nil {
		t.Fatalf("waiting for replayed watches: %v", err)
	}

	cancel()
	_ = grp.Wait()
}

// makeCRD generates a CRD specified by GVK and plural for testing.
func makeCRD(gvk schema.GroupVersionKind, plural string) *apiextensionsv1.CustomResourceDefinition {
	trueBool := true
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s.%s", plural, gvk.Group),
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "CustomResourceDefinition",
			APIVersion: "apiextensions/v1",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: gvk.Group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   plural,
				Singular: strings.ToLower(gvk.Kind),
				Kind:     gvk.Kind,
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    gvk.Version,
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							XPreserveUnknownFields: &trueBool,
						},
					},
				},
			},
			Scope: apiextensionsv1.ClusterScoped,
		},
	}
}

// applyCRD applies a CRD and waits for it to register successfully.
func applyCRD(ctx context.Context, t *testing.T, client client.Client, gvk schema.GroupVersionKind, crd client.Object) error {
	err := client.Create(ctx, crd)
	if err != nil {
		return fmt.Errorf("creating %+v: %w", gvk, err)
	}

	u := &unstructured.UnstructuredList{}
	u.SetGroupVersionKind(gvk)
	require.Eventually(t, func() bool {
		if ctx.Err() != nil {
			return false
		}

		return client.List(ctx, u) == nil
	}, 5*time.Second, 100*time.Millisecond, "waiting for CRD to be registered")
	return nil
}

// waitForCRDToUnregister waits for a CRD to be unregistered from the API server.
func waitForCRDToUnregister(ctx context.Context, cfg *rest.Config, gvk schema.GroupVersionKind, plural string) error {
	// Create a new clientset to avoid any client caching of discovery
	cs, err := clientset.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("creating clientset: %w", err)
	}
loop:
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Get the Resources for this GroupVersion
		resourceList, err := cs.Discovery().ServerResourcesForGroupVersion(gvk.GroupVersion().String())
		if err != nil {
			if apierrors.IsNotFound(err) {
				// This is normal when unregistering the last resource in a group.
				return nil
			}
			return fmt.Errorf("getting resources for group: %+v: %w", gvk.GroupVersion(), err)
		}

		for i := range resourceList.APIResources {
			if resourceList.APIResources[i].Name == plural {
				select {
				case <-time.After(100 * time.Millisecond):
				case <-ctx.Done():
				}
				continue loop
			}
		}
		return nil
	}
}

// unstructuredFor returns an Unstructured resource of the requested kind and name.
func unstructuredFor(gvk schema.GroupVersionKind, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	u.SetName(name)
	return u
}

// unstructuredList generates a list of n resources prefixed with the provided name.
func unstructuredList(gvk schema.GroupVersionKind, prefix string, n int) []*unstructured.Unstructured {
	if n < 1 {
		return nil
	}
	out := make([]*unstructured.Unstructured, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("%s-%d", prefix, i)
		u := unstructuredFor(gvk, name)
		out[i] = u
	}
	return out
}

// withNamespace returns a list corresponding to the input but with the specified namespace set.
func withNamespace(in []*unstructured.Unstructured, namespace string) []*unstructured.Unstructured {
	if in == nil {
		return nil
	}
	out := make([]*unstructured.Unstructured, len(in))
	for i := range in {
		out[i] = in[i].DeepCopy()
		out[i].SetNamespace(namespace)
	}
	return out
}

// expectedSet creates a set of names given a list of objects.
func expectedSet(objs []*unstructured.Unstructured) map[string]bool {
	out := make(map[string]bool, len(objs))
	for _, o := range objs {
		out[o.GetName()] = true
	}
	return out
}

// waitForExpected waits for reconcile requests for the specified resources to be received in a particular namespace.
// Returns nil if expectations are satisfied.
// Returns error if the context is canceled before expectations are satisfied.
func waitForExpected(ctx context.Context, objs []*unstructured.Unstructured, c <-chan reconcile.Request, namespace string) error {
	expected := expectedSet(objs)
	for len(expected) > 0 {
		select {
		case req := <-c:
			if req.Namespace != namespace {
				continue
			}
			delete(expected, req.Name)
		case <-ctx.Done():
			return context.Canceled
		}
	}
	return nil
}
