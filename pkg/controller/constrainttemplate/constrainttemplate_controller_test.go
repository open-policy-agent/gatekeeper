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
	"sync"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
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
)

const timeout = time.Second * 15

// setupManager sets up a controller-runtime manager with registered watch manager.
func setupManager(t *testing.T) (manager.Manager, *watch.Manager) {
	t.Helper()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
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

	// Uncommenting the below enables logging of K8s internals like watch.
	// klog.InitFlags(fs)
	// fs.Parse([]string{"--alsologtostderr", "-v=10"})
	// klog.SetOutput(os.Stderr)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, wm := setupManager(t)
	c := mgr.GetClient()
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
	tracker, err := readiness.SetupTracker(mgr)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	rec, _ := newReconciler(mgr, opa, wm, cs, tracker)
	g.Expect(add(mgr, rec)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)
	once := sync.Once{}
	testMgrStopped := func() {
		once.Do(func() {
			close(stopMgr)
			mgrStopped.Wait()
		})
	}

	defer testMgrStopped()

	t.Run("CRD Gets Created", func(t *testing.T) {
		err = c.Create(ctx, instance)
		g.Expect(err).NotTo(gomega.HaveOccurred())

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
	})

	t.Run("Constraint is marked as enforced", func(t *testing.T) {
		err = c.Create(ctx, newDenyAllCstr())
		g.Expect(err).NotTo(gomega.HaveOccurred())
		constraintEnforced(c, g, timeout)
	})

	t.Run("Constraint actually enforced", func(t *testing.T) {
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
	})
	t.Run("Deleted constraint CRDs are recreated", func(t *testing.T) {
		crd := &apiextensionsv1beta1.CustomResourceDefinition{}
		g.Expect(c.Get(ctx, crdKey, crd)).NotTo(gomega.HaveOccurred())
		origUID := crd.GetUID()
		crd.Spec = apiextensionsv1beta1.CustomResourceDefinitionSpec{}
		g.Expect(c.Delete(ctx, crd)).NotTo(gomega.HaveOccurred())

		g.Eventually(func() error {
			crd := &apiextensionsv1beta1.CustomResourceDefinition{}
			if err := c.Get(ctx, crdKey, crd); err != nil {
				return err
			}
			if !crd.GetDeletionTimestamp().IsZero() {
				return errors.New("Still deleting")
			}
			if crd.GetUID() == origUID {
				return errors.New("Not yet deleted")
			}
			for _, cond := range crd.Status.Conditions {
				if cond.Type == apiextensionsv1beta1.Established && cond.Status == apiextensionsv1beta1.ConditionTrue {
					return nil
				}
			}
			return errors.New("Not established")
		}, timeout, time.Second).Should(gomega.BeNil())
		g.Eventually(func() error { return c.Create(ctx, newDenyAllCstr()) }, timeout).Should(gomega.BeNil())
		// we need a longer timeout because deleting the CRD interrupts the watch
		constraintEnforced(c, g, 50*timeout)
	})

	t.Run("Templates with Invalid Rego throw errors", func(t *testing.T) {
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

		err = c.Create(ctx, instanceInvalidRego)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			err = c.Delete(ctx, instanceInvalidRego)
			g.Expect(err).NotTo(gomega.HaveOccurred())
		}()

		g.Eventually(func() error {
			ct := &v1beta1.ConstraintTemplate{}
			if err := c.Get(ctx, types.NamespacedName{Name: "invalidrego"}, ct); err != nil {
				return err
			}
			if ct.Name == "invalidrego" {
				status := util.GetCTHAStatus(ct)
				if len(status.Errors) == 0 {
					j, err := json.Marshal(status)
					if err != nil {
						t.Fatal("could not parse JSON", err)
					}
					s := string(j)
					return fmt.Errorf("InvalidRego template should contain an error: %s", s)
				}
				if status.Errors[0].Code != "rego_parse_error" {
					return fmt.Errorf("InvalidRego template returning unexpected error %s", status.Errors[0].Code)
				}
				return nil
			}
			return errors.New("InvalidRego not found")
		}, timeout).Should(gomega.BeNil())
	})

	t.Run("Deleted constraint templates not enforced", func(t *testing.T) {
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
		g.Expect(c.Delete(ctx, instance.DeepCopy())).Should(gomega.BeNil())
		g.Eventually(func() error {
			resp, err := opa.Review(ctx, req)
			if err != nil {
				return err
			}
			if len(resp.Results()) != 0 {
				dump, _ := opa.Dump(ctx)
				return fmt.Errorf("Results not yet zero\nOPA DUMP:\n%s", dump)
			}
			return nil
		}, timeout).Should(gomega.BeNil())
	})
}

func constraintEnforced(c client.Client, g *gomega.GomegaWithT, timeout time.Duration) {
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
