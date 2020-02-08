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

package constrainttemplate

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/constraint"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	constraintutil "github.com/open-policy-agent/gatekeeper/pkg/util/constraint"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"github.com/open-policy-agent/gatekeeper/third_party/sigs.k8s.io/controller-runtime/pkg/dynamiccache"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{Name: "denyall"}}

var expectedRequestInvalidRego = reconcile.Request{NamespacedName: types.NamespacedName{Name: "invalidrego"}}

const timeout = time.Second * 15

func newClient(cfg *rest.Config) client.Client {
	c, err := client.New(cfg, client.Options{})
	if err != nil {
		panic(err)
	}
	return c
}

func constraintEnforced(g *gomega.GomegaWithT) {
	g.Eventually(func() error {
		cstr := newDenyAllCstr()
		err := c.Get(context.TODO(), types.NamespacedName{Name: "denyallconstraint"}, cstr)
		if err != nil {
			return err
		}
		status, err := constraintutil.GetHAStatus(cstr)
		if err != nil {
			return err
		}
		if !status.Enforced {
			return errors.New("constraint not enforced")
		}
		return nil
	}, timeout).Should(gomega.BeNil())
}

func newDenyAllCstr() *unstructured.Unstructured {
	cstr := &unstructured.Unstructured{}
	cstr.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "constraints.gatekeeper.sh",
		Version: "v1beta1",
		Kind:    "DenyAll",
	})
	cstr.SetName("denyallconstraint")
	return cstr
}

// setupManager sets up a controller-runtime manager with registered watch manager.
func setupManager(t *testing.T) (manager.Manager, *watch.Manager) {
	t.Helper()

	ctrl.SetLogger(zap.Logger(true))
	metrics.Registry = prometheus.NewRegistry()
	mgr, err := manager.New(cfg, manager.Options{
		MetricsBindAddress: "0",
		NewCache:           dynamiccache.New,
		MapperProvider: func(c *rest.Config) (meta.RESTMapper, error) {
			return apiutil.NewDynamicRESTMapper(c)
		},
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

func TestReconcile(t *testing.T) {
	crdKey := types.NamespacedName{Name: "denyall.constraints.gatekeeper.sh"}
	g := gomega.NewGomegaWithT(t)
	instance := &v1beta1.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "denyall"},
		Spec: v1beta1.ConstraintTemplateSpec{
			CRD: v1beta1.CRD{
				Spec: v1beta1.CRDSpec{
					Names: v1beta1.Names{
						Kind: "DenyAll",
					},
				},
			},
			Targets: []v1beta1.Target{
				{
					Target: "admission.k8s.gatekeeper.sh",
					Rego: `
package foo

violation[{"msg": "denied!"}] {
	1 == 1
}
`},
			},
		},
	}

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, wm := setupManager(t)
	c := mgr.GetClient()
	ctrl.SetLogger(zap.Logger(true))
	ctx := context.Background()

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

	cs := watch.NewSwitch()
	rec, _ := newReconciler(mgr, opa, wm, cs)
	recFn, requests := SetupTestReconcile(rec)
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)
	once := sync.Once{}
	testMgrStopped := func() {
		once.Do(func() {
			close(stopMgr)
			mgrStopped.Wait()
		})
	}

	defer testMgrStopped()

	// Create the ConstraintTemplate object and expect the CRD to be created
	log.Info("CREATING CONSTRAINT TEMPLATE")
	err = c.Create(ctx, instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer func() {
		err := c.Delete(ctx, instance)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}()
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	clientset := kubernetes.NewForConfigOrDie(cfg)
	g.Eventually(func() error {
		crd := &apiextensionsv1beta1.CustomResourceDefinition{}
		if err := c.Get(ctx, crdKey, crd); err != nil {
			return err
		}
		rs, err := clientset.Discovery().ServerResourcesForGroupVersion("constraints.gatekeeper.sh/v1beta1")
		if err != nil {
			return err
		}
		for _, r := range rs.APIResources {
			if r.Kind == "DenyAll" {
				return nil
			}
		}
		return errors.New("DenyAll not found")
	}, timeout).Should(gomega.BeNil())

	err = c.Create(ctx, newDenyAllCstr())
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer func() {
		err = c.Delete(ctx, newDenyAllCstr())
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	g.Eventually(func() error {
		o := &unstructured.Unstructured{}
		err := c.Get(ctx, types.NamespacedName{Name: "denyall"}, o)
		if err != nil {
			return err
		}
		status, err := constraintutil.GetHAStatus(o)
		if err != nil {
			return err
		}
		if !status.Enforced {
			return errors.New("constraint not enforced")
		}
		return nil
	}, timeout).Should(gomega.BeNil())
	constraintEnforced(g)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "testns",
		},
	}
	req := admissionv1beta1.AdmissionRequest{
		Kind: metav1.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "Namespace",
		},
		Operation: "Create",
		Name:      "FooNamespace",
		Object:    runtime.RawExtension{Object: ns},
	}
	resp, err := opa.Review(ctx, req)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	if len(resp.Results()) != 1 {
		fmt.Println(resp.TraceDump())
		fmt.Println(opa.Dump(ctx))
	}
	g.Expect(len(resp.Results())).Should(gomega.Equal(1))

	// If the CRD is deleted out from underneath the template, it is recreated
	log.Info("TESTING CRD RECREATION ON DELETE")
	crd := &apiextensionsv1beta1.CustomResourceDefinition{}
	crd.SetName(crdKey.Name)
	crd.Spec = apiextensionsv1beta1.CustomResourceDefinitionSpec{}
	g.Expect(c.Delete(ctx, crd)).NotTo(gomega.HaveOccurred())

	g.Eventually(func() error {
		// Drain the request queue so the controller can operate
		select {
		case v := <-requests:
			log.Info("Reconciling request", "request", v)
		default:
		}
		crd := &apiextensionsv1beta1.CustomResourceDefinition{}
		if err := c.Get(ctx, crdKey, crd); err != nil {
			return err
		}
		if !crd.GetDeletionTimestamp().IsZero() {
			return errors.New("Still deleting")
		}
		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1beta1.Established && cond.Status == apiextensionsv1beta1.ConditionTrue {
				return nil
			}
		}
		return errors.New("Not established")
	}, timeout, time.Second).Should(gomega.BeNil())

	g.Eventually(func() error { return c.Create(ctx, newDenyAllCstr()) }, timeout).Should(gomega.BeNil())
	constraintEnforced(g)

	// Create template with invalid rego, should populate parse error in status
	log.Info("TESTING INVALID REGO")
	instanceInvalidRego := &v1beta1.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "invalidrego"},
		Spec: v1beta1.ConstraintTemplateSpec{
			CRD: v1beta1.CRD{
				Spec: v1beta1.CRDSpec{
					Names: v1beta1.Names{
						Kind: "InvalidRego",
					},
				},
			},
			Targets: []v1beta1.Target{
				{
					Target: "admission.k8s.gatekeeper.sh",
					Rego: `
package foo

violation[{"msg": "hi"}] { 1 == 1 }

anyrule[}}}//invalid//rego

`},
			},
		},
	}

	err = c.Create(ctx, instanceInvalidRego)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer func() {
		err = c.Delete(ctx, instanceInvalidRego)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}()
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequestInvalidRego)))

	g.Eventually(func() error {
		ct := &v1beta1.ConstraintTemplate{}
		if err := c.Get(ctx, types.NamespacedName{Name: "invalidrego"}, ct); err != nil {
			return err
		}
		if ct.Name == "invalidrego" {
			status := util.GetCTHAStatus(ct)
			if len(status.Errors) != 1 {
				return errors.New("InvalidRego template should contain 1 parse error")
			}
			if status.Errors[0].Code != "rego_parse_error" {
				return fmt.Errorf("InvalidRego template returning unexpected error %s", status.Errors[0].Code)
			}
			return nil
		}
		return errors.New("InvalidRego not found")
	}, timeout).Should(gomega.BeNil())

	// Test finalizer removal
	log.Info("TESTING FINALIZER REMOVAL")
	orig := &v1beta1.ConstraintTemplate{}
	g.Expect(c.Get(ctx, types.NamespacedName{Name: "denyall"}, orig)).NotTo(gomega.HaveOccurred())
	g.Expect(containsString(finalizerName, orig.GetFinalizers())).Should(gomega.BeTrue())

	origCstr := newDenyAllCstr()
	g.Eventually(func() error {
		err := c.Get(ctx, types.NamespacedName{Name: "denyallconstraint"}, origCstr)
		if err != nil {
			return err
		}
		if !constraint.HasFinalizer(origCstr) {
			return errors.New("Waiting on constraint")
		}
		return nil
	}, timeout).Should(gomega.BeNil())

	testMgrStopped()
	cs.Stop()
	time.Sleep(5 * time.Second)

	finished := make(chan struct{})
	newCli, err := client.New(mgr.GetConfig(), client.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	TearDownState(newCli, finished)
	<-finished

	g.Eventually(func() error {
		obj := &v1beta1.ConstraintTemplate{}
		if err := newCli.Get(ctx, types.NamespacedName{Name: "denyall"}, obj); err != nil {
			return err
		}
		if containsString(finalizerName, obj.GetFinalizers()) {
			return errors.New("denyall constraint template still has finalizer")
		}
		if len(obj.Status.ByPod) != 0 {
			return errors.New("denyall constraint template still has pod-specific status")
		}
		return nil
	}, timeout).Should(gomega.BeNil())

	cleanCstr := origCstr.DeepCopy()
	g.Eventually(func() error {
		err := c.Get(ctx, types.NamespacedName{Name: "denyallconstraint"}, cleanCstr)
		if err != nil {
			return err
		}
		if constraint.HasFinalizer(cleanCstr) {
			return errors.New("denyall constraint still has finalizer")
		}
		s, exists, err := unstructured.NestedSlice(cleanCstr.Object, "status", "byPod")
		if err != nil {
			return fmt.Errorf("unstructured access error: %v", err)
		}
		if !exists {
			return nil
		}
		if len(s) != 0 {
			return fmt.Errorf("byPod status is not empty: %v", s)
		}
		return nil
	}, timeout).Should(gomega.BeNil())
	log.Info("EXITING")
}
