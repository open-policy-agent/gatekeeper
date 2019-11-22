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
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/constraint"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"golang.org/x/net/context"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var c client.Client

var expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{Name: "denyall"}}

var expectedRequestInvalidRego = reconcile.Request{NamespacedName: types.NamespacedName{Name: "invalidrego"}}

const timeout = time.Second * 5

func TestReconcile(t *testing.T) {
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
	rec, _ := newReconciler(mgr, opa, watch.New(ctx, mgr.GetConfig()))
	recFn, requests := SetupTestReconcile(rec)
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	// Create the ConstraintTemplate object and expect the CRD to be created
	err = c.Create(context.TODO(), instance)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer func() {
		err := c.Delete(context.TODO(), instance)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}()
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	clientset := kubernetes.NewForConfigOrDie(cfg)
	g.Eventually(func() error {
		crd := &apiextensionsv1beta1.CustomResourceDefinition{}
		if err := c.Get(context.TODO(), types.NamespacedName{Name: "denyall.constraints.gatekeeper.sh"}, crd); err != nil {
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

	cstr := &unstructured.Unstructured{}
	cstr.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "constraints.gatekeeper.sh",
		Version: "v1beta1",
		Kind:    "DenyAll",
	})
	cstr.SetName("denyall")
	kindSelector := `[{"apiGroups": ["*"], "kinds": ["*"]}]`
	ks := make([]interface{}, 0)
	err = json.Unmarshal([]byte(kindSelector), &ks)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	err = unstructured.SetNestedSlice(cstr.Object, ks, "spec", "match", "kinds")
	g.Expect(err).NotTo(gomega.HaveOccurred())

	dynamic := dynamic.NewForConfigOrDie(cfg)
	cstrClient := dynamic.Resource(schema.GroupVersionResource{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Resource: "denyall"})
	_, err = cstrClient.Create(cstr, metav1.CreateOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer func() {
		err = cstrClient.Delete(cstr.GetName(), &metav1.DeleteOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	g.Eventually(func() error {
		o, err := cstrClient.Get("denyall", metav1.GetOptions{TypeMeta: metav1.TypeMeta{Kind: "DenyAll", APIVersion: "constraints.gatekeeper.sh/v1beta1"}})
		if err != nil {
			return err
		}
		status, err := util.GetHAStatus(o)
		if err != nil {
			return err
		}
		val, found, err := unstructured.NestedBool(status, "enforced")
		if err != nil {
			return err
		}
		if !found {
			return errors.New("status not found")
		}
		if !val {
			return errors.New("constraint not enforced")
		}
		return nil
	}, timeout).Should(gomega.BeNil())

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
	resp, err := opa.Review(context.TODO(), req)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	if len(resp.Results()) != 1 {
		fmt.Println(resp.TraceDump())
		fmt.Println(opa.Dump(context.TODO()))
	}
	g.Expect(len(resp.Results())).Should(gomega.Equal(1))

	// Create template with invalid rego, should populate parse error in status
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

	err = c.Create(context.TODO(), instanceInvalidRego)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer func() {
		err = c.Delete(context.TODO(), instanceInvalidRego)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	}()
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequestInvalidRego)))

	g.Eventually(func() error {
		ct := &v1beta1.ConstraintTemplate{}
		if err := c.Get(context.TODO(), types.NamespacedName{Name: "invalidrego"}, ct); err != nil {
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
	orig := &v1beta1.ConstraintTemplate{}
	g.Expect(c.Get(context.TODO(), types.NamespacedName{Name: "denyall"}, orig)).NotTo(gomega.HaveOccurred())
	g.Expect(containsString(finalizerName, orig.GetFinalizers())).Should(gomega.BeTrue())

	origCstr, err := cstrClient.Get("denyall", metav1.GetOptions{TypeMeta: metav1.TypeMeta{Kind: "DenyAll", APIVersion: "constraints.gatekeeper.sh/v1beta1"}})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(constraint.HasFinalizer(origCstr)).Should(gomega.BeTrue())

	cancel()
	time.Sleep(5 * time.Second)
	finished := make(chan struct{})
	newCli, err := client.New(mgr.GetConfig(), client.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	RemoveAllFinalizers(newCli, finished)
	<-finished

	g.Eventually(func() error {
		obj := &v1beta1.ConstraintTemplate{}
		if err := c.Get(context.TODO(), types.NamespacedName{Name: "denyall"}, obj); err != nil {
			return err
		}
		if containsString(finalizerName, obj.GetFinalizers()) {
			return errors.New("denyall constraint template still has finalizer")
		}
		return nil
	}, timeout).Should(gomega.BeNil())

	g.Eventually(func() error {
		cleanCstr, err := cstrClient.Get("denyall", metav1.GetOptions{TypeMeta: metav1.TypeMeta{Kind: "DenyAll", APIVersion: "constraints.gatekeeper.sh/v1beta1"}})
		if err != nil {
			return err
		}
		if constraint.HasFinalizer(cleanCstr) {
			return errors.New("denyall constraint still has finalizer")
		}
		return nil
	}, timeout).Should(gomega.BeNil())
}
