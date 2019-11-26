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

package config

import (
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/onsi/gomega"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/api/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/sync"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"golang.org/x/net/context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var c client.Client

var expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{
	Name:      "config",
	Namespace: "gatekeeper-system",
}}

const timeout = time.Second * 20

func TestReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	instance := &configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: "gatekeeper-system",
		},
		Spec: configv1alpha1.ConfigSpec{
			Sync: configv1alpha1.Sync{
				SyncOnly: []configv1alpha1.SyncOnlyEntry{
					{Group: "", Version: "v1", Kind: "Namespace"},
					{Group: "", Version: "v1", Kind: "Pod"},
				},
			},
		},
	}

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	// initialize OPA
	driver := local.New(local.Tracing(true))
	backend, err := opa.NewBackend(opa.Driver(driver))
	if err != nil {
		t.Fatalf("unable to set up OPA backend: %s", err)

	}
	opa, err := backend.NewClient(opa.Targets(&target.K8sValidationTarget{}))
	if err != nil {
		t.Fatalf("unable to set up OPA client: %s", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher := watch.New(ctx, mgr.GetConfig())
	rec, _ := newReconciler(mgr, opa, watcher)
	recFn, requests := SetupTestReconcile(rec)
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	// Create the Config object and expect the Reconcile to be created
	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	defer func() {
		err = c.Delete(context.TODO(), instance)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}()
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	gvks, err := watcher.GetManagedGVK()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Eventually(len(gvks), timeout).ShouldNot(gomega.Equal(0))

	sort.Slice(gvks, func(i, j int) bool { return gvks[i].Kind < gvks[j].Kind })

	g.Expect(gvks).Should(gomega.Equal([]schema.GroupVersionKind{
		{Group: "", Version: "v1", Kind: "Namespace"},
		{Group: "", Version: "v1", Kind: "Pod"},
	}))

	ns := &unstructured.Unstructured{}
	ns.SetName("testns")
	nsGvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
	ns.SetGroupVersionKind(nsGvk)
	g.Expect(c.Create(context.TODO(), ns)).NotTo(gomega.HaveOccurred())

	// Test finalizer removal
	orig := &configv1alpha1.Config{}
	g.Expect(c.Get(context.TODO(), CfgKey, orig)).NotTo(gomega.HaveOccurred())
	g.Expect(hasFinalizer(orig)).Should(gomega.BeTrue())

	g.Eventually(func() error {
		ns := &unstructured.Unstructured{}
		ns.SetGroupVersionKind(nsGvk)
		if err := c.Get(context.Background(), types.NamespacedName{Name: "testns"}, ns); err != nil {
			return err
		}
		if !sync.HasFinalizer(ns) {
			return errors.New("namespace has no sync finalizer")
		}
		return nil
	}, timeout).Should(gomega.BeNil())

	cancel()
	time.Sleep(1 * time.Second)
	finished := make(chan struct{})
	newCli, err := client.New(mgr.GetConfig(), client.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	RemoveAllConfigFinalizers(newCli, finished)
	<-finished
	time.Sleep(1 * time.Second)

	g.Eventually(func() error {
		obj := &configv1alpha1.Config{}
		if err := c.Get(context.TODO(), CfgKey, obj); err != nil {
			return err
		}
		if hasFinalizer(obj) {
			return errors.New("config resource still has sync finalizer")
		}
		return nil
	}, timeout).Should(gomega.BeNil())

	g.Eventually(func() error {
		cleanNs := &unstructured.Unstructured{}
		if err := c.Get(context.Background(), types.NamespacedName{Name: "testns"}, ns); err != nil {
			return err
		}
		if sync.HasFinalizer(cleanNs) {
			return errors.New("testns namespace still has sync finalizer")
		}
		return nil
	}, timeout).Should(gomega.BeNil())

}
