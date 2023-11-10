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

package watch

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kcache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

type fakeCacheInformer struct {
	mu       sync.Mutex
	handlers map[kcache.ResourceEventHandler]int
}

func (f *fakeCacheInformer) AddEventHandler(h kcache.ResourceEventHandler) (kcache.ResourceEventHandlerRegistration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.handlers == nil {
		f.handlers = make(map[kcache.ResourceEventHandler]int)
	}
	f.handlers[h]++
	return nil, nil
}

func (f *fakeCacheInformer) RemoveEventHandler(handle kcache.ResourceEventHandlerRegistration) error {
	return nil
}

func (f *fakeCacheInformer) AddEventHandlerWithResyncPeriod(h kcache.ResourceEventHandler, resyncPeriod time.Duration) (kcache.ResourceEventHandlerRegistration, error) {
	return f.AddEventHandler(h)
}

func (f *fakeCacheInformer) AddIndexers(indexers kcache.Indexers) error {
	return errors.New("not implemented")
}

func (f *fakeCacheInformer) HasSynced() bool {
	return false
}

func (f *fakeCacheInformer) totalHandlers() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	var total int
	for _, v := range f.handlers {
		total += v
	}

	return total
}

type fakeRemovableCache struct {
	mu            sync.Mutex
	informer      cache.Informer
	items         []unstructured.Unstructured
	removeCounter int
}

func (f *fakeRemovableCache) GetInformer(_ context.Context, obj client.Object, opts ...cache.InformerGetOption) (cache.Informer, error) {
	return f.informer, nil
}

func (f *fakeRemovableCache) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	switch v := list.(type) {
	case *unstructured.UnstructuredList:
		v.Items = f.items
	default:
		return fmt.Errorf("unexpected list type: %T. Needed unstructured.UnstructuredList", list)
	}
	return nil
}

func (f *fakeRemovableCache) Remove(obj client.Object) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removeCounter++
	return nil
}

func (f *fakeRemovableCache) removeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.removeCounter
}

type funcCache struct {
	ListFunc                   func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
	GetInformerNonBlockingFunc func(ctx context.Context, obj client.Object) (cache.Informer, error)
}

func (f *funcCache) GetInformer(ctx context.Context, obj client.Object, opts ...cache.InformerGetOption) (cache.Informer, error) {
	if f.GetInformerNonBlockingFunc != nil {
		return f.GetInformerNonBlockingFunc(ctx, obj)
	}
	return &fakeCacheInformer{}, nil
}

func (f *funcCache) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if f.ListFunc != nil {
		return f.ListFunc(ctx, list, opts...)
	}
	return errors.New("ListFunc not initialized")
}

func (f *funcCache) Remove(obj client.Object) error {
	return nil
}

func setupWatchManager(c RemovableCache) (*Manager, context.CancelFunc, error) {
	wm, err := New(c)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	grp, ctx := errgroup.WithContext(ctx)
	grp.Go(func() error {
		return wm.Start(ctx)
	})

	shutdown := func() {
		cancel()
		_ = grp.Wait()
	}
	return wm, shutdown, nil
}

// Verifies that redundant calls to AddWatch (even across registrars) will be idempotent
// and only register a single event handler on the respective informer.
func TestRegistrar_AddWatch_Idempotent(t *testing.T) {
	ctx := context.Background()
	informer := &fakeCacheInformer{}
	c := &fakeRemovableCache{informer: informer}
	wm, cancel, err := setupWatchManager(c)
	if err != nil {
		t.Errorf("creating watch manager: %v", err)
		return
	}
	t.Cleanup(cancel)

	r1, err := wm.NewRegistrar("r1", make(chan event.GenericEvent, 1))
	if err != nil {
		t.Errorf("creating registrar: %v", err)
		return
	}
	r2, err := wm.NewRegistrar("r2", make(chan event.GenericEvent, 1))
	if err != nil {
		t.Errorf("creating registrar: %v", err)
		return
	}

	gvk := schema.GroupVersionKind{
		Version: "v1",
		Kind:    "Pod",
	}

	for _, r := range []*Registrar{r1, r2} {
		if err := r.AddWatch(ctx, gvk); err != nil {
			t.Errorf("setting initial watch: %v", err)
			return
		}
		if err := r.AddWatch(ctx, gvk); err != nil {
			t.Errorf("setting redundant watch: %v", err)
			return
		}
	}
	managed := wm.managedKinds.Get()
	expected := vitalsByGVK{
		gvk: vitals{
			gvk:        gvk,
			registrars: map[*Registrar]bool{r1: true, r2: true},
		},
	}
	if !reflect.DeepEqual(expected, managed) {
		t.Errorf("unexpected manged set: %+v", expected)
	}

	if total := informer.totalHandlers(); total != 1 {
		t.Errorf("unexpected handler count. got: %d, expected: 1", total)
	}
}

func TestRegistrar_RemoveWatch_Idempotent(t *testing.T) {
	ctx := context.Background()
	informer := &fakeCacheInformer{}
	c := &fakeRemovableCache{informer: informer}
	wm, cancel, err := setupWatchManager(c)
	if err != nil {
		t.Errorf("creating watch manager: %v", err)
		return
	}
	t.Cleanup(cancel)

	r1, err := wm.NewRegistrar("r1", make(chan event.GenericEvent, 1))
	if err != nil {
		t.Errorf("creating registrar: %v", err)
		return
	}
	r2, err := wm.NewRegistrar("r2", make(chan event.GenericEvent, 1))
	if err != nil {
		t.Errorf("creating registrar: %v", err)
		return
	}

	gvk := schema.GroupVersionKind{
		Version: "v1",
		Kind:    "Pod",
	}

	for _, r := range []*Registrar{r1, r2} {
		if err := r.AddWatch(ctx, gvk); err != nil {
			t.Errorf("setting initial watch: %v", err)
			return
		}
	}
	managed := wm.GetManagedGVK()
	expected := []schema.GroupVersionKind{gvk}
	if !reflect.DeepEqual(expected, managed) {
		t.Errorf("unexpected managed set: %+v", expected)
		return
	}

	if err := r1.RemoveWatch(ctx, gvk); err != nil {
		t.Errorf("removing first watch: %v", err)
		return
	}

	// Should still be watching this kind (reference count > 0).
	managed = wm.GetManagedGVK()
	if !reflect.DeepEqual(expected, managed) {
		t.Errorf("unexpected managed set after removing first registrar: %+v", expected)
	}

	// The informer should not have been removed due to remaining watch.
	if c.removeCount() != 0 {
		t.Errorf("informer was removed before last watch was removed")
		return
	}

	if err := r2.RemoveWatch(ctx, gvk); err != nil {
		t.Errorf("removing second watch: %v", err)
		return
	}

	// Should no longer be watching.
	managed = wm.GetManagedGVK()
	if len(managed) > 0 {
		t.Errorf("unexpected manged set after removing last registrar: %+v", expected)
		return
	}

	// The informer should have been removed this time.
	if c.removeCount() != 1 {
		t.Errorf("informer was not removed after last watch was removed")
		return
	}

	// Extra removes are fine.
	if err := r2.RemoveWatch(ctx, gvk); err != nil {
		t.Errorf("redundant remove: %v", err)
		return
	}
	if c.removeCount() != 1 {
		t.Errorf("informer should not have been removed twice")
		return
	}
}

// Verify that existing items are replayed when joining an existing watched resource.
func TestRegistrar_Replay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	gvk := schema.GroupVersionKind{
		Version: "v1",
		Kind:    "Pod",
	}
	informer := &fakeCacheInformer{}
	resources := generateTestResources(gvk, 10)
	c := &fakeRemovableCache{informer: informer, items: resources}
	wm, cancel, err := setupWatchManager(c)
	if err != nil {
		t.Errorf("creating watch manager: %v", err)
		return
	}
	t.Cleanup(cancel)

	const count = 4
	type tuple struct {
		r *Registrar
		e chan event.GenericEvent
	}

	registrars := make([]tuple, count)
	for i := 0; i < count; i++ {
		e := make(chan event.GenericEvent, len(resources))
		r, err := wm.NewRegistrar(fmt.Sprintf("r%d", i), e)
		registrars[i] = tuple{r: r, e: e}
		if err != nil {
			t.Fatalf("creating registrar: %v", err)
		}
	}

	for _, entry := range registrars {
		if err := entry.r.AddWatch(ctx, gvk); err != nil {
			t.Errorf("setting initial watch: %v", err)
			return
		}
	}

	for i, entry := range registrars {
		if i == 0 {
			// Separate check for the first watcher below
			continue
		}
		// Expect to get events on the latter watchers via replay
		for i := range resources {
			select {
			case <-ctx.Done():
				t.Errorf("timeout waiting for replayed resources [%s]", entry.r.parentName)
				return
			case event, ok := <-entry.e:
				if !ok {
					t.Errorf("channel closed while waiting for resources [%s]", entry.r.parentName)
					return
				}
				gotName := event.Object.GetName()
				wantName := resources[i].GetName()
				if gotName != wantName {
					t.Fatalf("got name %v, want %v", gotName, wantName)
				}
			}
		}
	}

	// Expect no events on the first watcher (we're pretending these were created after the fact
	// and our fakes don't actually call event handlers)
	select {
	case event := <-registrars[0].e:
		t.Errorf("received unexpected event from first watcher: %v", event)
		return
	case <-time.After(50 * time.Millisecond):
		// Success
	}
}

// Verify that event replay can retry upon error.
func TestRegistrar_Replay_Retry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	gvk := schema.GroupVersionKind{
		Version: "v1",
		Kind:    "Pod",
	}
	informer := &fakeCacheInformer{}
	resources := generateTestResources(gvk, 10)
	errCount := 3
	c := &funcCache{
		ListFunc: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
			if errCount > 0 {
				errCount--
				return fmt.Errorf("failing %d more times", errCount)
			}
			switch v := list.(type) {
			case *unstructured.UnstructuredList:
				v.Items = resources
			default:
				return fmt.Errorf("unexpected list type: %T. Needed unstructured.UnstructuredList", list)
			}
			return nil
		},
		GetInformerNonBlockingFunc: func(_ context.Context, obj client.Object) (cache.Informer, error) {
			return informer, nil
		},
	}
	wm, cancel, err := setupWatchManager(c)
	if err != nil {
		t.Errorf("creating watch manager: %v", err)
		return
	}
	t.Cleanup(cancel)

	e1 := make(chan event.GenericEvent, 1)
	r1, err := wm.NewRegistrar("r1", e1)
	if err != nil {
		t.Errorf("creating registrar: %v", err)
		return
	}
	e2 := make(chan event.GenericEvent, len(resources))
	r2, err := wm.NewRegistrar("r2", e2)
	if err != nil {
		t.Errorf("creating registrar: %v", err)
		return
	}

	for _, r := range []*Registrar{r1, r2} {
		if err := r.AddWatch(ctx, gvk); err != nil {
			t.Errorf("setting initial watch: %v", err)
			return
		}
	}

	// Expect to get events on the second watch via replay, even after some failures.
	for i := range resources {
		select {
		case <-ctx.Done():
			t.Errorf("timeout waiting for replayed resources")
			return
		case event, ok := <-e2:
			if !ok {
				t.Errorf("channel closed while waiting for resources")
				return
			}

			gotName := event.Object.GetName()
			wantName := resources[i].GetName()
			if gotName != wantName {
				t.Fatalf("got object name %q, want %q", gotName, wantName)
			}
		}
	}

	// Expect no events on the first watcher (we're pretending these were created after the fact
	// and our fakes don't actually call event handlers)
	select {
	case event := <-e1:
		t.Errorf("received unexpected event from first watcher: %v", event)
		return
	case <-time.After(50 * time.Millisecond):
		// Success
	}
}

// Verifies that replay happens asynchronously, can be canceled.
func TestRegistrar_Replay_Async(t *testing.T) {
	listCalled := make(chan struct{})
	listDone := make(chan struct{})
	c := &funcCache{
		ListFunc: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
			listCalled <- struct{}{}

			// Block until we're canceled.
			<-ctx.Done()
			listDone <- struct{}{}
			return nil
		},
	}

	// Setup and start watch manager
	wm, err := New(c)
	if err != nil {
		t.Fatalf("creating watch manager: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	grp, ctx := errgroup.WithContext(ctx)
	grp.Go(func() error {
		return wm.Start(ctx)
	})

	// "Primary" watcher. Doesn't trigger replay.
	e1 := make(chan event.GenericEvent)
	r1, err := wm.NewRegistrar("r1", e1)
	if err != nil {
		t.Errorf("creating registrar: %v", err)
		return
	}

	// When this watcher adds its watch, replay will be triggered.
	e2 := make(chan event.GenericEvent)
	r2, err := wm.NewRegistrar("r2", e2)
	if err != nil {
		t.Errorf("creating registrar: %v", err)
		return
	}

	gvk := schema.GroupVersionKind{
		Version: "v1",
		Kind:    "Pod",
	}
	for _, r := range []*Registrar{r1, r2} {
		if err := r.AddWatch(ctx, gvk); err != nil {
			t.Errorf("setting initial watch: %v", err)
			return
		}
	}

	// Ensure list was called (and we didn't block in AddWatch)
	select {
	case <-listCalled:
	// Good.
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("list was not called by replay as expected")
	}

	// Ensure we can cancel a pending replay
	if err := r2.RemoveWatch(ctx, gvk); err != nil {
		t.Errorf("removing watch: %v", err)
	}

	select {
	case <-listDone:
	// Good.
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("replay was not canceled")
	}

	// [Scenario 2] - Verify that pending replays are canceled during watch manager shutdown.
	if err := r2.AddWatch(ctx, gvk); err != nil {
		t.Errorf("adding watch: %v", err)
	}
	select {
	case <-listCalled:
	// Good.
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("list was not called by replay as expected")
	}

	// Shutdown watch manager and expect replays to cancel.
	cancel()
	select {
	case <-listDone:
	// Good.
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("replay was not canceled")
	}

	_ = grp.Wait()
}

// Verifies that registrar names must be unique.
func TestRegistrar_Duplicates_Rejected(t *testing.T) {
	informer := &fakeCacheInformer{}
	c := &fakeRemovableCache{informer: informer}
	wm, cancel, err := setupWatchManager(c)
	if err != nil {
		t.Errorf("creating watch manager: %v", err)
		return
	}
	t.Cleanup(cancel)

	_, err = wm.NewRegistrar("dup", make(chan event.GenericEvent, 1))
	if err != nil {
		t.Fatal(err)
	}
	_, err = wm.NewRegistrar("dup", make(chan event.GenericEvent, 1))
	if err == nil {
		t.Fatalf("expected duplicate error")
	}
}

// Verify ReplaceWatch replaces the set of watched resources for a registrar. New watches will be added,
// unneeded watches will be removed, and watches that haven't changed will remain unchanged.
func TestRegistrar_ReplaceWatch(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	listCalls := make(map[schema.GroupVersionKind]int)
	getInformerCalls := make(map[schema.GroupVersionKind]int)
	c := &funcCache{
		ListFunc: func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
			mu.Lock()
			defer mu.Unlock()
			gvk := list.GetObjectKind().GroupVersionKind()
			gvk.Kind = strings.TrimSuffix(gvk.Kind, "List")
			listCalls[gvk]++
			return nil
		},
		GetInformerNonBlockingFunc: func(_ context.Context, obj client.Object) (cache.Informer, error) {
			mu.Lock()
			defer mu.Unlock()
			gvk := obj.GetObjectKind().GroupVersionKind()
			getInformerCalls[gvk]++
			return &fakeCacheInformer{}, nil
		},
	}
	wm, cancel, err := setupWatchManager(c)
	if err != nil {
		t.Errorf("creating watch manager: %v", err)
		return
	}
	t.Cleanup(cancel)

	r1, err := wm.NewRegistrar("r1", make(chan event.GenericEvent))
	if err != nil {
		t.Fatal(err)
	}
	r2, err := wm.NewRegistrar("r2", make(chan event.GenericEvent))
	if err != nil {
		t.Fatal(err)
	}

	pod := schema.GroupVersionKind{Version: "v1", Kind: "Pod"}
	volume := schema.GroupVersionKind{Version: "v1", Kind: "Volume"}
	deploy := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	configMap := schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}
	service := schema.GroupVersionKind{Version: "v1", Kind: "Service"}
	secret := schema.GroupVersionKind{Version: "v1", Kind: "Secret"}

	err = r1.AddWatch(ctx, pod)
	if err != nil {
		t.Fatalf("initial pod watch: %v", err)
	}
	err = r1.AddWatch(ctx, volume)
	if err != nil {
		t.Fatalf("initial volume watch: %v", err)
	}
	err = r1.AddWatch(ctx, deploy)
	if err != nil {
		t.Fatalf("initial deployment watch: %v", err)
	}

	err = r2.AddWatch(ctx, volume)
	if err != nil {
		t.Fatalf("initial volume watch: %v", err)
	}
	err = r2.AddWatch(ctx, configMap)
	if err != nil {
		t.Fatalf("initial configmap watch: %v", err)
	}
	err = r2.AddWatch(ctx, secret)
	if err != nil {
		t.Fatalf("initial secret watch: %v", err)
	}

	// Check initial counters
	func() {
		mu.Lock()
		defer mu.Unlock()
		// There should only be a single GetInformer call, even with multiple watchers
		if getInformerCalls[pod] != 1 {
			t.Fatalf("got getInformerCalls[pod] = %v, want %v", getInformerCalls[pod], 1)
		}
		if getInformerCalls[volume] != 1 {
			t.Fatalf("got getInformerCalls[volume] = %v, want %v", getInformerCalls[volume], 1)
		}
		if getInformerCalls[deploy] != 1 {
			t.Fatalf("got getInformerCalls[deploy] = %v, want %v", getInformerCalls[deploy], 1)
		}
		if getInformerCalls[configMap] != 1 {
			t.Fatalf("got getInformerCalls[configMap] = %v, want %v", getInformerCalls[configMap], 1)
		}
		if getInformerCalls[secret] != 1 {
			t.Fatalf("got getInformerCalls[secret] = %v, want %v", getInformerCalls[secret], 1)
		}
		if getInformerCalls[service] != 0 {
			t.Fatalf("got getInformerCalls[service] = %v, want %v", getInformerCalls[service], 0)
		}

		// When a second registrar watches the same resource, it will trigger a replay (and thus a List request)
		if listCalls[pod] != 0 {
			t.Fatalf("got listCalls[pod] = %v, want %v", listCalls[pod], 0)
		}
	}()

	// Pod overlaps between r1 and r2. Secret is retained. ConfigMap is swapped for Service.
	// Volume originally overlapped between r1 and r2, but will be removed from r2.
	err = r2.ReplaceWatch(ctx, []schema.GroupVersionKind{pod, service, secret})
	if err != nil {
		t.Fatalf("calling replaceWatch: %v", err)
	}

	// Check final counters
	func() {
		mu.Lock()
		defer mu.Unlock()
		if getInformerCalls[pod] != 1 {
			t.Fatalf("got getInformerCalls[pod] = %v, want %v", getInformerCalls[pod], 1)
		}
		if getInformerCalls[volume] != 1 {
			t.Fatalf("got getInformerCalls[volume] = %v, want %v", getInformerCalls[volume], 1)
		}
		if getInformerCalls[deploy] != 1 {
			t.Fatalf("got getInformerCalls[deploy] = %v, want %v", getInformerCalls[deploy], 1)
		}
		if getInformerCalls[configMap] != 1 {
			t.Fatalf("got getInformerCalls[configMap] = %v, want %v", getInformerCalls[configMap], 1)
		}
		if getInformerCalls[service] != 1 {
			t.Fatalf("got getInformerCalls[service] = %v, want %v", getInformerCalls[service], 1)
		}
		if getInformerCalls[secret] != 1 {
			t.Fatalf("got getInformerCalls[secret] = %v, want %v", getInformerCalls[secret], 1)
		}
	}()
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		return listCalls[pod] == 1
	}, 5*time.Second, 1*time.Second, "final pod replay count")

	// Replay should not be called against deployment - it should not leak from r1 to r2.
	func() {
		// require consistently
		timeoutAfter := time.After(5 * time.Second)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				func() {
					mu.Lock()
					defer mu.Unlock()

					require.Equal(t, 0, listCalls[deploy], "final deployment replay count")
				}()
			case <-timeoutAfter:
				return
			}
		}
	}()

	// Verify internals
	registrarCounts := map[schema.GroupVersionKind]int{
		pod:       2,
		volume:    1,
		deploy:    1,
		configMap: 0,
		secret:    1,
		service:   1,
	}
	wm.watchedMux.Lock()
	defer wm.watchedMux.Unlock()
	for gvk, count := range registrarCounts {
		registrars := wm.watchedKinds[gvk].registrars
		if len(registrars) != count {
			t.Fatalf("got registrar %v count %v, want %v", gvk, len(registrars), count)
		}
	}
}

func generateTestResources(gvk schema.GroupVersionKind, n int) []unstructured.Unstructured {
	if n == 0 {
		return nil
	}
	out := make([]unstructured.Unstructured, n)
	for i := 0; i < n; i++ {
		out[i].SetGroupVersionKind(gvk)
		out[i].SetName(fmt.Sprintf("%s-%d", gvk.Kind, i))
	}
	return out
}
