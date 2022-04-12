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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/onsi/gomega"
	mutationsinternal "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	mutationsv1beta1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1beta1"
	podstatus "github.com/open-policy-agent/gatekeeper/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/mutatorstatus"
	"github.com/open-policy-agent/gatekeeper/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators"
	mutationschema "github.com/open-policy-agent/gatekeeper/pkg/mutation/schema"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/test/testutils"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apiTypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const timeout = time.Second * 15

func makeValue(v interface{}) mutationsv1beta1.AssignField {
	return mutationsv1beta1.AssignField{Value: &types.Anything{Value: v}}
}

// setupManager sets up a controller-runtime manager with registered watch manager.
func setupManager(t *testing.T) manager.Manager {
	t.Helper()

	metrics.Registry = prometheus.NewRegistry()
	mgr, err := manager.New(cfg, manager.Options{
		MetricsBindAddress: "0",
		MapperProvider: func(c *rest.Config) (meta.RESTMapper, error) {
			return apiutil.NewDynamicRESTMapper(c)
		},
		Logger: testutils.NewLogger(t),
	})
	if err != nil {
		t.Fatalf("setting up controller manager: %s", err)
	}
	return mgr
}

func newAssign(name, location, value string) *mutationsv1beta1.Assign {
	return &mutationsv1beta1.Assign{
		TypeMeta: metav1.TypeMeta{
			APIVersion: mutationsv1beta1.GroupVersion.String(),
			Kind:       "Assign",
		},
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: mutationsv1beta1.AssignSpec{
			ApplyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"ConfigMap"}}},
			Location: location,
			Parameters: mutationsv1beta1.Parameters{
				Assign: makeValue(value),
			},
		},
	}
}

func TestReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	mutator := &mutationsv1beta1.Assign{
		ObjectMeta: metav1.ObjectMeta{
			Name: "assign-test-obj",
		},
		Spec: mutationsv1beta1.AssignSpec{
			ApplyTo:  []match.ApplyTo{{Groups: []string{""}, Versions: []string{"v1"}, Kinds: []string{"ConfigMap"}}},
			Location: "spec.test",
			Parameters: mutationsv1beta1.Parameters{
				Assign: makeValue("works"),
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
	if err := createGatekeeperNamespace(mgr.GetConfig()); err != nil {
		t.Fatalf("want createGatekeeperNamespace(mgr.GetConfig()) error = nil, got %v", err)
	}

	mSys := mutation.NewSystem(mutation.SystemOpts{})

	tracker, err := readiness.SetupTracker(mgr, true, false)
	if err != nil {
		t.Fatal(err)
	}
	testutils.Setenv(t, "POD_NAME", "no-pod")

	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	kind := "Assign"
	newObj := func() client.Object { return &mutationsv1beta1.Assign{} }
	newMutator := func(obj client.Object) (types.Mutator, error) {
		assign := obj.(*mutationsv1beta1.Assign) // nolint:forcetypeassert
		unversioned := &mutationsinternal.Assign{}
		if err := mgr.GetScheme().Convert(assign, unversioned, nil); err != nil {
			return nil, err
		}
		return mutators.MutatorForAssign(unversioned)
	}
	events := make(chan event.GenericEvent, 1024)

	rec := newReconciler(mgr, mSys, tracker, func(ctx context.Context) (*corev1.Pod, error) { return pod, nil }, kind, newObj, newMutator, events)
	adder := Adder{EventsSource: &source.Channel{Source: events}}

	err = adder.add(mgr, rec)
	if err != nil {
		t.Fatal(err)
	}

	statusAdder := &mutatorstatus.Adder{}
	err = statusAdder.Add(mgr)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	t.Run("Can add a mutator", func(t *testing.T) {
		err := c.Create(ctx, mutator.DeepCopy())
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Mutator is reported as enforced", func(t *testing.T) {
		g.Eventually(func() error {
			v := &mutationsv1beta1.Assign{}
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
		_, err := mSys.Mutate(&types.Mutable{Object: u})
		if err != nil {
			t.Fatal(err)
		}

		v, exists, err := unstructured.NestedString(u.Object, "spec", "test")
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatal("mutated value is missing")
		}
		if v != "works" {
			t.Fatalf(`value = %s, expected "works"`, v)
		}
	})

	t.Run("Mutation deletion is honored", func(t *testing.T) {
		err := c.Delete(ctx, mutator.DeepCopy())
		if err != nil {
			t.Fatal(err)
		}
		g.Eventually(func() error {
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})

			_, err := mSys.Mutate(&types.Mutable{Object: u})
			if err != nil {
				t.Fatal(err)
			}

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

		err := c.Create(ctx, mFoo.DeepCopy())
		if err != nil {
			t.Fatal(err)
		}

		err = c.Create(ctx, mBar1.DeepCopy())
		if err != nil {
			t.Fatal(err)
		}

		g.Eventually(func() error {
			err := podStatusMatches(ctx, c, pod, mFooID, hasStatusErrors(nil))
			if err != nil {
				return err
			}

			return podStatusMatches(ctx, c, pod, mBar1ID, hasStatusErrors(nil))
		})

		err = c.Create(ctx, mBar2.DeepCopy())
		if err != nil {
			t.Fatal(err)
		}

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

		err = c.Delete(ctx, mBar1.DeepCopy())
		if err != nil {
			t.Fatal(err)
		}

		g.Eventually(func() error {
			err := podStatusMatches(ctx, c, pod, mFooID, hasStatusErrors(nil))
			if err != nil {
				return err
			}

			return podStatusMatches(ctx, c, pod, mBar2ID, hasStatusErrors(nil))
		})
	})
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
