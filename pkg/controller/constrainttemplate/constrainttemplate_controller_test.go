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
	"strings"
	"testing"
	"time"

	templatesv1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	podstatus "github.com/open-policy-agent/gatekeeper/apis/status/v1beta1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	testclient "github.com/open-policy-agent/gatekeeper/test/clients"
	"github.com/open-policy-agent/gatekeeper/test/testutils"
	"github.com/open-policy-agent/gatekeeper/third_party/sigs.k8s.io/controller-runtime/pkg/dynamiccache"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// constantRetry makes 3,000 attempts at a rate of 100 per second. Since this
// is a test instance and not a "real" cluster, this is fine and there's no need
// to increase the wait time each iteration.
var constantRetry = wait.Backoff{
	Steps:    3000,
	Duration: 10 * time.Millisecond,
}

// setupManager sets up a controller-runtime manager with registered watch manager.
func setupManager(t *testing.T) (manager.Manager, *watch.Manager) {
	t.Helper()

	metrics.Registry = prometheus.NewRegistry()
	mgr, err := manager.New(cfg, manager.Options{
		MetricsBindAddress: "0",
		NewCache:           dynamiccache.New,
		MapperProvider: func(c *rest.Config) (meta.RESTMapper, error) {
			return apiutil.NewDynamicRESTMapper(c)
		},
		Logger: testutils.NewLogger(t),
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

func makeReconcileConstraintTemplate(suffix string) *v1beta1.ConstraintTemplate {
	return &v1beta1.ConstraintTemplate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConstraintTemplate",
			APIVersion: templatesv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{Name: "denyall" + strings.ToLower(suffix)},
		Spec: v1beta1.ConstraintTemplateSpec{
			CRD: v1beta1.CRD{
				Spec: v1beta1.CRDSpec{
					Names: v1beta1.Names{
						Kind: "DenyAll" + suffix,
					},
				},
			},
			Targets: []v1beta1.Target{
				{
					Target: target.Name,
					Rego: `
package foo

violation[{"msg": "denied!"}] {
	1 == 1
}
`,
				},
			},
		},
	}
}

func crdKey(suffix string) types.NamespacedName {
	return types.NamespacedName{Name: fmt.Sprintf("denyall%s.constraints.gatekeeper.sh", strings.ToLower(suffix))}
}

func expectedCRD(suffix string) *apiextensions.CustomResourceDefinition {
	crd := &apiextensions.CustomResourceDefinition{}
	key := crdKey(suffix)
	crd.SetName(key.Name)
	crd.SetGroupVersionKind(apiextensionsv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"))
	return crd
}

func TestReconcile(t *testing.T) {
	// Uncommenting the below enables logging of K8s internals like watch.
	// fs := flag.NewFlagSet("", flag.PanicOnError)
	// klog.InitFlags(fs)
	// fs.Parse([]string{"--alsologtostderr", "-v=10"})
	// klog.SetOutput(os.Stderr)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, wm := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())

	// creating the gatekeeper-system namespace is necessary because that's where
	// status resources live by default
	err := createGatekeeperNamespace(mgr.GetConfig())
	if err != nil {
		t.Fatal(err)
	}

	// initialize OPA
	driver, err := local.New(local.Tracing(true))
	if err != nil {
		t.Fatalf("unable to set up Driver: %v", err)
	}

	opaClient, err := constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver))
	if err != nil {
		t.Fatalf("unable to set up OPA client: %s", err)
	}

	testutils.Setenv(t, "POD_NAME", "no-pod")

	cs := watch.NewSwitch()
	tracker, err := readiness.SetupTracker(mgr, false, false)
	if err != nil {
		t.Fatal(err)
	}

	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	// events will be used to receive events from dynamic watches registered
	events := make(chan event.GenericEvent, 1024)
	rec, err := newReconciler(mgr, opaClient, wm, cs, tracker, events, events, func(context.Context) (*corev1.Pod, error) { return pod, nil })
	if err != nil {
		t.Fatal(err)
	}

	err = add(mgr, rec)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	t.Run("CRD Gets Created", func(t *testing.T) {
		suffix := "CRDGetsCreated"

		logger.Info("Running test: CRD Gets Created")
		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		t.Cleanup(deleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		createThenCleanup(ctx, t, c, constraintTemplate)

		clientset := kubernetes.NewForConfigOrDie(cfg)
		err = retry.OnError(constantRetry, func(err error) bool {
			return true
		}, func() error {
			crd := &apiextensionsv1.CustomResourceDefinition{}
			if err := c.Get(ctx, crdKey(suffix), crd); err != nil {
				return err
			}
			rs, err := clientset.Discovery().ServerResourcesForGroupVersion("constraints.gatekeeper.sh/v1beta1")
			if err != nil {
				return err
			}
			for _, r := range rs.APIResources {
				if r.Kind == "DenyAll"+suffix {
					return nil
				}
			}
			return errors.New("DenyAll not found")
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Constraint is marked as enforced", func(t *testing.T) {
		suffix := "MarkedEnforced"

		logger.Info("Running test: Constraint is marked as enforced")
		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		cstr := newDenyAllCstr(suffix)

		t.Cleanup(deleteObjectAndConfirm(ctx, t, c, cstr))
		t.Cleanup(deleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		createThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(constantRetry, func(error) bool {
			return true
		}, func() error {
			return c.Create(ctx, cstr)
		})
		if err != nil {
			t.Fatal(err)
		}

		err = constraintEnforced(ctx, c, suffix)
		if err != nil {
			t.Fatal(err)
		}

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testns",
			},
		}
		req := admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Namespace",
			},
			Operation: "Create",
			Name:      "FooNamespace",
			Object:    runtime.RawExtension{Object: ns},
		}
		resp, err := opaClient.Review(ctx, req)
		if err != nil {
			t.Fatal(err)
		}

		gotResults := resp.Results()
		if len(gotResults) != 1 {
			t.Log(resp.TraceDump())
			t.Log(opaClient.Dump(ctx))
			t.Fatalf("want 1 result, got %v", gotResults)
		}
	})

	t.Run("Deleted constraint CRDs are recreated", func(t *testing.T) {
		suffix := "CRDRecreated"

		logger.Info("Running test: Deleted constraint CRDs are recreated")
		// Clean up to remove the crd, constraint and constraint template
		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		cstr := newDenyAllCstr(suffix)

		t.Cleanup(deleteObjectAndConfirm(ctx, t, c, cstr))
		t.Cleanup(deleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		createThenCleanup(ctx, t, c, constraintTemplate)

		var crd *apiextensionsv1.CustomResourceDefinition
		err = retry.OnError(constantRetry, func(err error) bool {
			return true
		}, func() error {
			crd = &apiextensionsv1.CustomResourceDefinition{}
			return c.Get(ctx, crdKey(suffix), crd)
		})
		if err != nil {
			t.Fatal(err)
		}

		origUID := crd.GetUID()
		crd.Spec = apiextensionsv1.CustomResourceDefinitionSpec{}
		err = c.Delete(ctx, crd)
		if err != nil {
			t.Fatal(err)
		}

		err = retry.OnError(constantRetry, func(err error) bool {
			return true
		}, func() error {
			crd := &apiextensionsv1.CustomResourceDefinition{}
			if err := c.Get(ctx, crdKey(suffix), crd); err != nil {
				return err
			}
			if !crd.GetDeletionTimestamp().IsZero() {
				return errors.New("still deleting")
			}
			if crd.GetUID() == origUID {
				return errors.New("not yet deleted")
			}
			for _, cond := range crd.Status.Conditions {
				if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
					return nil
				}
			}
			return errors.New("not established")
		})
		if err != nil {
			t.Fatal(err)
		}

		err = retry.OnError(constantRetry, func(err error) bool {
			return true
		}, func() error {
			sList := &podstatus.ConstraintPodStatusList{}
			if err := c.List(ctx, sList); err != nil {
				return err
			}
			if len(sList.Items) != 0 {
				return fmt.Errorf("remaining status items: %+v", sList.Items)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		err = retry.OnError(constantRetry, func(err error) bool {
			return true
		}, func() error {
			return c.Create(ctx, newDenyAllCstr(suffix))
		})
		if err != nil {
			t.Fatal(err)
		}

		// we need a longer timeout because deleting the CRD interrupts the watch
		err = constraintEnforced(ctx, c, suffix)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Templates with Invalid Rego throw errors", func(t *testing.T) {
		logger.Info("Running test: Templates with Invalid Rego throw errors")
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
						Target: target.Name,
						Rego: `
	package foo

	violation[{"msg": "hi"}] { 1 == 1 }

	anyrule[}}}//invalid//rego

	`,
					},
				},
			},
		}

		err = c.Create(ctx, instanceInvalidRego)
		if err != nil {
			t.Fatal(err)
		}

		// TODO: Test if this removal is necessary.
		// https://github.com/open-policy-agent/gatekeeper/pull/1595#discussion_r722819552
		t.Cleanup(testutils.DeleteObject(t, c, instanceInvalidRego))

		err = retry.OnError(constantRetry, func(err error) bool {
			return true
		}, func() error {
			ct := &v1beta1.ConstraintTemplate{}
			if err := c.Get(ctx, types.NamespacedName{Name: "invalidrego"}, ct); err != nil {
				return err
			}

			if ct.Name != "invalidrego" {
				return errors.New("InvalidRego not found")
			}

			status, found := getCTByPodStatus(ct)
			if !found {
				return fmt.Errorf("could not retrieve CT status for pod, byPod status: %+v", ct.Status.ByPod)
			}

			if len(status.Errors) == 0 {
				j, err := json.Marshal(status)
				if err != nil {
					t.Fatal("could not parse JSON", err)
				}
				s := string(j)
				return fmt.Errorf("InvalidRego template should contain an error: %q", s)
			}

			if status.Errors[0].Code != ErrIngestCode {
				return fmt.Errorf("InvalidRego template returning unexpected error %q, got error %+v",
					status.Errors[0].Code, status.Errors)
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Deleted constraint templates not enforced", func(t *testing.T) {
		suffix := "DeletedNotEnforced"

		logger.Info("Running test: Deleted constraint templates not enforced")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testns",
			},
		}
		req := admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Namespace",
			},
			Operation: "Create",
			Name:      "FooNamespace",
			Object:    runtime.RawExtension{Object: ns},
		}
		resp, err := opaClient.Review(ctx, req)
		if err != nil {
			t.Fatal(err)
		}

		gotResults := resp.Results()
		if len(resp.Results()) != 0 {
			t.Log(resp.TraceDump())
			t.Log(opaClient.Dump(ctx))
			t.Fatalf("did not get 0 results: %v", gotResults)
		}

		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		err = c.Delete(ctx, constraintTemplate)
		if err != nil && !apierrors.IsNotFound(err) {
			t.Fatal(err)
		}

		err = retry.OnError(constantRetry, func(err error) bool {
			return true
		}, func() error {
			resp, err := opaClient.Review(ctx, req)
			if err != nil {
				return err
			}
			if len(resp.Results()) != 0 {
				dump, _ := opaClient.Dump(ctx)
				return fmt.Errorf("Results not yet zero\nOPA DUMP:\n%s", dump)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

// Tests that expectations for constraints are canceled if the corresponding constraint is deleted.
func TestReconcile_DeleteConstraintResources(t *testing.T) {
	logger.Info("Running test: Cancel the expectations when constraint gets deleted")

	// Setup the Manager
	mgr, wm := setupManager(t)
	c := testclient.NewRetryClient(mgr.GetClient())

	// start manager that will start tracker and controller
	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	// Create the constraint template object and expect Reconcile to be called
	// when the controller starts.
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
					Target: target.Name,
					Rego: `
package foo

violation[{"msg": "denied!"}] {
	1 == 1
}
`,
				},
			},
		},
	}

	err := c.Create(ctx, instance)
	if err != nil {
		t.Fatal(err)
	}

	// TODO: Test if this removal is necessary.
	// https://github.com/open-policy-agent/gatekeeper/pull/1595#discussion_r722819552
	t.Cleanup(testutils.DeleteObject(t, c, instance))

	gvk := schema.GroupVersionKind{
		Group:   "constraints.gatekeeper.sh",
		Version: "v1beta1",
		Kind:    "DenyAll",
	}

	// Install constraint CRD
	crd := makeCRD(gvk, "denyall")
	err = applyCRD(ctx, c, gvk, crd)
	if err != nil {
		t.Fatalf("applying CRD: %v", err)
	}

	// Create the constraint for constraint template
	cstr := newDenyAllCstr("")
	err = c.Create(ctx, cstr)
	if err != nil {
		t.Fatal(err)
	}

	// creating the gatekeeper-system namespace is necessary because that's where
	// status resources live by default
	err = createGatekeeperNamespace(mgr.GetConfig())
	if err != nil {
		t.Fatal(err)
	}

	// Set up tracker
	tracker, err := readiness.SetupTracker(mgr, false, false)
	if err != nil {
		t.Fatal(err)
	}

	// initialize OPA
	driver, err := local.New(local.Tracing(true))
	if err != nil {
		t.Fatalf("unable to set up Driver: %v", err)
	}

	opaClient, err := constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver))
	if err != nil {
		t.Fatalf("unable to set up OPA client: %s", err)
	}

	testutils.Setenv(t, "POD_NAME", "no-pod")

	cs := watch.NewSwitch()
	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	// events will be used to receive events from dynamic watches registered
	events := make(chan event.GenericEvent, 1024)
	rec, err := newReconciler(mgr, opaClient, wm, cs, tracker, events, nil, func(context.Context) (*corev1.Pod, error) { return pod, nil })
	if err != nil {
		t.Fatal(err)
	}

	err = add(mgr, rec)
	if err != nil {
		t.Fatal(err)
	}

	// get the object tracker for the constraint
	ot := tracker.For(gvk)
	tr, ok := ot.(testExpectations)
	if !ok {
		t.Fatalf("unexpected tracker, got %T", ot)
	}
	// ensure that expectations are set for the constraint gvk
	err = retry.OnError(constantRetry, func(err error) bool {
		return true
	}, func() error {
		gotExpected := tr.IsExpecting(gvk, types.NamespacedName{Name: "denyallconstraint"})
		if !gotExpected {
			return errors.New("waiting for expectations")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Delete the constraint , the delete event will be reconciled by controller
	// to cancel the expectation set for it by tracker
	err = c.Delete(ctx, cstr)
	if err != nil {
		t.Fatal(err)
	}

	// set event channel to receive request for constraint
	events <- event.GenericEvent{
		Object: cstr,
	}

	// Check readiness tracker is satisfied post-reconcile
	err = retry.OnError(constantRetry, func(err error) bool {
		return true
	}, func() error {
		satisfied := tracker.For(gvk).Satisfied()
		if !satisfied {
			return errors.New("not satisfied")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func constraintEnforced(ctx context.Context, c client.Client, suffix string) error {
	return retry.OnError(constantRetry, func(err error) bool {
		return true
	}, func() error {
		cstr := newDenyAllCstr(suffix)
		err := c.Get(ctx, types.NamespacedName{Name: "denyallconstraint"}, cstr)
		if err != nil {
			return err
		}
		status, err := getCByPodStatus(cstr)
		if err != nil {
			return err
		}
		if !status.Enforced {
			obj, _ := json.MarshalIndent(cstr.Object, "", "  ")
			// return errors.New("constraint not enforced)
			return fmt.Errorf("constraint not enforced: \n%s", obj)
		}
		return nil
	})
}

func newDenyAllCstr(suffix string) *unstructured.Unstructured {
	cstr := &unstructured.Unstructured{}
	cstr.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "constraints.gatekeeper.sh",
		Version: "v1beta1",
		Kind:    "DenyAll" + suffix,
	})
	cstr.SetName("denyallconstraint")
	return cstr
}

func getCTByPodStatus(templ *v1beta1.ConstraintTemplate) (v1beta1.ByPodStatus, bool) {
	statuses := templ.Status.ByPod
	for _, s := range statuses {
		if s.ID == util.GetID() {
			return s, true
		}
	}
	return v1beta1.ByPodStatus{}, false
}

func getCByPodStatus(obj *unstructured.Unstructured) (*statusv1beta1.ConstraintPodStatusStatus, error) {
	list, found, err := unstructured.NestedSlice(obj.Object, "status", "byPod")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("no byPod status is set")
	}
	var status *statusv1beta1.ConstraintPodStatusStatus
	for _, v := range list {
		j, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		s := &statusv1beta1.ConstraintPodStatusStatus{}
		if err := json.Unmarshal(j, s); err != nil {
			return nil, err
		}
		if s.ID == util.GetID() {
			status = s
			break
		}
	}
	if status == nil {
		return nil, errors.New("current pod is not listed in byPod status")
	}
	return status, nil
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
func applyCRD(ctx context.Context, client client.Client, gvk schema.GroupVersionKind, crd client.Object) error {
	err := client.Create(ctx, crd)
	if err != nil {
		return err
	}

	u := &unstructured.UnstructuredList{}
	u.SetGroupVersionKind(gvk)
	return retry.OnError(constantRetry, func(err error) bool {
		return true
	}, func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return client.List(ctx, u)
	})
}

// deleteObjectAndConfirm returns a callback which deletes obj from the passed
// Client. Does result in mutations to obj. The callback includes a cached copy
// of all information required to delete obj in the callback, so it is safe to
// mutate obj afterwards. Similarly - client.Delete mutates its input, but
// the callback does not call client.Delete on obj. Instead, it creates a
// single-purpose Unstructured for this purpose. Thus, obj is not mutated after
// the callback is run.
func deleteObjectAndConfirm(ctx context.Context, t *testing.T, c client.Client, obj client.Object) func() {
	t.Helper()

	// Cache the identifying information from obj. We refer to this cached
	// information in the callback, and not obj itself.
	gvk := obj.GetObjectKind().GroupVersionKind()
	namespace := obj.GetNamespace()
	name := obj.GetName()

	if gvk.Empty() {
		// We can't send a proper delete request with an Unstructured without
		// filling in GVK. The alternative would be to require tests to construct
		// a valid Scheme or provide a factory method for the type to delete - this
		// is easier.
		t.Fatalf("gvk for %v/%v %T is empty",
			namespace, name, obj)
	}

	return func() {
		t.Helper()

		// Construct a single-use Unstructured to send the Delete request.
		toDelete := makeUnstructured(gvk, namespace, name)
		err := c.Delete(ctx, toDelete)
		if apierrors.IsNotFound(err) {
			return
		} else if err != nil {
			t.Fatal(err)
		}

		err = retry.OnError(constantRetry, func(err error) bool {
			return true
		}, func() error {
			// Construct a single-use Unstructured to send the Get request. It isn't
			// safe to reuse Unstructureds for each retry as Get modifies its input.
			toGet := makeUnstructured(gvk, namespace, name)
			key := client.ObjectKey{Namespace: namespace, Name: name}
			err2 := c.Get(ctx, key, toGet)
			if apierrors.IsGone(err2) || apierrors.IsNotFound(err2) {
				return nil
			}

			// Marshal the currently-gotten object, so it can be printed in test
			// failure output.
			s, _ := json.MarshalIndent(toGet, "", "  ")
			return fmt.Errorf("found %v %v:\n%s", gvk, key, string(s))
		})

		if err != nil {
			t.Fatal(err)
		}
	}
}

// This interface is getting used by tests to check the private objects of objectTracker.
type testExpectations interface {
	IsExpecting(gvk schema.GroupVersionKind, nsName types.NamespacedName) bool
}

// createThenCleanup creates obj in Client, and then registers obj to be deleted
// at the end of the test. The passed obj is safely deepcopied before being
// passed to client.Create, so it is not mutated by this call.
func createThenCleanup(ctx context.Context, t *testing.T, c client.Client, obj client.Object) {
	t.Helper()
	cpy := obj.DeepCopyObject()
	cpyObj, ok := cpy.(client.Object)
	if !ok {
		t.Fatalf("got obj.DeepCopyObject() type = %T, want %T", cpy, client.Object(nil))
	}

	err := c.Create(ctx, cpyObj)
	if err != nil {
		t.Fatal(err)
	}

	// It is unnecessary to deepcopy obj as deleteObjectAndConfirm does not pass
	// obj to any Client calls.
	t.Cleanup(deleteObjectAndConfirm(ctx, t, c, obj))
}

func makeUnstructured(gvk schema.GroupVersionKind, namespace, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{
		Object: make(map[string]interface{}),
	}
	u.SetGroupVersionKind(gvk)
	u.SetNamespace(namespace)
	u.SetName(name)
	return u
}
