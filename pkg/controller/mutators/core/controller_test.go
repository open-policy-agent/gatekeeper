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

package core

import (
	"fmt"
	"github.com/open-policy-agent/gatekeeper/test/testcleanups"
	"os"
	gosync "sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/onsi/gomega"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	podstatus "github.com/open-policy-agent/gatekeeper/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/mutatorstatus"
	"github.com/open-policy-agent/gatekeeper/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators"
	mutationschema "github.com/open-policy-agent/gatekeeper/pkg/mutation/schema"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apiTypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const timeout = time.Second * 15

// setupManager sets up a controller-runtime manager with registered watch manager.
func setupManager(t *testing.T) manager.Manager {
	t.Helper()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(testcleanups.NewTestWriter(t))))
	metrics.Registry = prometheus.NewRegistry()
	mgr, err := manager.New(cfg, manager.Options{
		MetricsBindAddress: "0",
		MapperProvider: func(c *rest.Config) (meta.RESTMapper, error) {
			return apiutil.NewDynamicRESTMapper(c)
		},
	})
	if err != nil {
		t.Fatalf("setting up controller manager: %s", err)
	}
	return mgr
}

func newAssign(name, location, value string) *mutationsv1alpha1.Assign {
	return &mutationsv1alpha1.Assign{
		TypeMeta: metav1.TypeMeta{
			APIVersion: mutationsv1alpha1.GroupVersion.String(),
			Kind:       "Assign",
		},
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: mutationsv1alpha1.AssignSpec{
			ApplyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"ConfigMap"}}},
			Location: location,
			Parameters: mutationsv1alpha1.Parameters{
				Assign: runtime.RawExtension{Raw: []byte(fmt.Sprintf(`{"value": %q}`, value))},
			},
		},
	}
}

func TestReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	mutator := &mutationsv1alpha1.Assign{
		ObjectMeta: metav1.ObjectMeta{
			Name: "assign-test-obj",
		},
		Spec: mutationsv1alpha1.AssignSpec{
			ApplyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"ConfigMap"}}},
			Location: "spec.test",
			Parameters: mutationsv1alpha1.Parameters{
				Assign: runtime.RawExtension{Raw: []byte(`{"value": "works"}`)},
			},
		},
	}
	objName := apiTypes.NamespacedName{Name: "assign-test-obj"}

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr := setupManager(t)
	c := mgr.GetClient()

	// creating the gatekeeper-system namespace is necessary because that's where
	// status resources live by default
	g.Expect(createGatekeeperNamespace(mgr.GetConfig())).To(gomega.BeNil())

	// force mutation to be enabled
	*mutation.MutationEnabled = true

	mSys := mutation.NewSystem(mutation.SystemOpts{})

	tracker, err := readiness.SetupTracker(mgr, true, false)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	err = os.Setenv("POD_NAME", "no-pod")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(testcleanups.UnsetEnv(t, "POD_NAME"))

	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	kind := "Assign"
	newObj := func() client.Object { return &mutationsv1alpha1.Assign{} }
	newMutator := func(obj client.Object) (types.Mutator, error) {
		assign := obj.(*mutationsv1alpha1.Assign) // nolint:forcetypeassert
		return mutators.MutatorForAssign(assign)
	}
	events := make(chan event.GenericEvent, 1024)

	rec := newReconciler(mgr, mSys, tracker, func(ctx context.Context) (*corev1.Pod, error) { return pod, nil }, kind, newObj, newMutator, events)

	g.Expect(add(mgr, rec)).NotTo(gomega.HaveOccurred())
	statusAdder := &mutatorstatus.Adder{}
	g.Expect(statusAdder.Add(mgr)).NotTo(gomega.HaveOccurred())

	ctx, cancelFunc := context.WithCancel(context.Background())
	mgrStopped := StartTestManager(ctx, t, mgr)
	once := gosync.Once{}
	testMgrStopped := func() {
		once.Do(func() {
			cancelFunc()
			mgrStopped.Wait()
		})
	}

	defer testMgrStopped()

	t.Run("Can add a mutator", func(t *testing.T) {
		g.Expect(c.Create(ctx, mutator.DeepCopy())).NotTo(gomega.HaveOccurred())
	})

	t.Run("Mutator is reported as enforced", func(t *testing.T) {
		g.Eventually(func() error {
			v := &mutationsv1alpha1.Assign{}
			v.SetName("assign-test-obj")
			if err := c.Get(ctx, objName, v); err != nil {
				return errors.Wrap(err, "cannot get mutator")
			}
			if len(v.Status.ByPod) < 1 {
				return errors.Errorf("no pod status reported: %+v", v)
			}
			if !v.Status.ByPod[0].Enforced {
				return errors.New("pod not reported as enforced")
			}
			return nil
		}, timeout).Should(gomega.Succeed())
	})

	t.Run("System mutates a resource", func(t *testing.T) {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
		g.Expect(func() error {
			_, err := mSys.Mutate(u, nil)
			return err
		}()).NotTo(gomega.HaveOccurred())
		g.Expect(func() error {
			v, exists, err := unstructured.NestedString(u.Object, "spec", "test")
			if err != nil {
				return err
			}
			if !exists {
				return errors.New("mutated value is missing")
			}
			if v != "works" {
				return errors.Errorf(`value = %s, expected "works"`, v)
			}
			return nil
		}()).NotTo(gomega.HaveOccurred())
	})

	t.Run("Mutation deletion is honored", func(t *testing.T) {
		g.Expect(c.Delete(ctx, mutator.DeepCopy())).NotTo(gomega.HaveOccurred())
		g.Eventually(func() error {
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
			g.Expect(func() error {
				_, err := mSys.Mutate(u, nil)
				return err
			}()).NotTo(gomega.HaveOccurred())
			_, exists, err := unstructured.NestedString(u.Object, "spec", "test")
			if err != nil {
				return err
			}
			if exists {
				return errors.New("mutated value still exists")
			}
			return nil
		}, timeout).Should(gomega.Succeed())
	})

	t.Run("Conflicting mutators are marked not enforced and conflicts can be resolved", func(t *testing.T) {
		mFoo := newAssign("foo", "spec.foo", "value-1")
		mFooID := types.MakeID(mFoo)
		mBar1 := newAssign("bar-1", "spec.bar[name: bar].qux", "value-2")
		mBar1ID := types.MakeID(mBar1)
		mBar2 := newAssign("bar-2", "spec.bar.qux", "value-3")
		mBar2ID := types.MakeID(mBar2)

		g.Expect(c.Create(ctx, mFoo.DeepCopy())).NotTo(gomega.HaveOccurred())
		g.Expect(c.Create(ctx, mBar1.DeepCopy())).NotTo(gomega.HaveOccurred())

		g.Eventually(func() error {
			err := podStatusMatches(ctx, c, pod, mFooID, hasStatusErrors(nil))
			if err != nil {
				return err
			}

			return podStatusMatches(ctx, c, pod, mBar1ID, hasStatusErrors(nil))
		})

		g.Expect(c.Create(ctx, mBar2.DeepCopy())).NotTo(gomega.HaveOccurred())

		g.Eventually(func() error {
			err := podStatusMatches(ctx, c, pod, mFooID, hasStatusErrors(nil))
			if err != nil {
				return err
			}

			err = podStatusMatches(ctx, c, pod, mBar1ID, hasStatusErrors([]podstatus.MutatorError{{
				Type: mutationschema.ErrConflictingSchemaType,
				Message: mutationschema.NewErrConflictingSchema(mutationschema.IDSet{
					mBar1ID: true, mBar2ID: true,
				}).Error(),
			}}))
			if err != nil {
				return err
			}

			return podStatusMatches(ctx, c, pod, mBar2ID, hasStatusErrors([]podstatus.MutatorError{{
				Type: mutationschema.ErrConflictingSchemaType,
				Message: mutationschema.NewErrConflictingSchema(mutationschema.IDSet{
					mBar1ID: true, mBar2ID: true,
				}).Error(),
			}}))
		}, timeout).Should(gomega.Succeed())

		g.Expect(c.Delete(ctx, mBar1.DeepCopy())).NotTo(gomega.HaveOccurred())

		g.Eventually(func() error {
			err := podStatusMatches(ctx, c, pod, mFooID, hasStatusErrors(nil))
			if err != nil {
				return err
			}

			return podStatusMatches(ctx, c, pod, mBar2ID, hasStatusErrors(nil))
		})
	})

	testMgrStopped()
}

func podStatusMatches(ctx context.Context, c client.Client, pod *corev1.Pod, id types.ID, matchers ...PodStatusMatcher) error {
	podStatus := &podstatus.MutatorPodStatus{}

	podStatusName, err := podstatus.KeyForMutatorID(pod.Name, id)
	if err != nil {
		return err
	}

	key := client.ObjectKey{Namespace: pod.Namespace, Name: podStatusName}
	err = c.Get(ctx, key, podStatus)
	if err != nil {
		return err
	}

	for _, m := range matchers {
		err = m(podStatus)
		if err != nil {
			return err
		}
	}

	return nil
}

type PodStatusMatcher func(status *podstatus.MutatorPodStatus) error

func hasStatusErrors(want []podstatus.MutatorError) PodStatusMatcher {
	return func(status *podstatus.MutatorPodStatus) error {
		got := status.Status.Errors
		if diff := cmp.Diff(want, got, cmpopts.SortSlices(sortMutatorErrors), cmpopts.EquateEmpty()); diff != "" {
			return fmt.Errorf("unexpected difference in .status.errors for %q:\n%s", status.Name, diff)
		}
		if len(want) == 0 {
			if !status.Status.Enforced {
				return fmt.Errorf("no errors in .status.errors but Mutator is not enforced")
			}
		} else {
			if status.Status.Enforced {
				return fmt.Errorf("errors in .status.errors but Mutator is enforced")
			}
		}

		return nil
	}
}

func sortMutatorErrors(left, right podstatus.MutatorError) bool {
	return left.Message < right.Message
}
