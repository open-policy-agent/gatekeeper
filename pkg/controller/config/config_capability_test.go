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
	"context"
	"testing"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TestAdderSkipsPodsThatDoNotUseConfig pins the registration gate: a pod that
// neither excludes processes nor syncs data must not build the Config
// controller, while a pod that does must still fail loudly on a missing
// dependency.
func TestAdderSkipsPodsThatDoNotUseConfig(t *testing.T) {
	mgr, _ := setupManager(t)

	t.Run("mutation status only", func(t *testing.T) {
		t.Cleanup(operations.AssignForTest(operations.MutationStatus))

		// Neither a cache manager nor a process excluder is set, so building the
		// reconciler would fail: a nil error proves nothing was registered.
		require.NoError(t, (&Adder{}).Add(mgr))
	})

	t.Run("mutation webhook", func(t *testing.T) {
		t.Cleanup(operations.AssignForTest(operations.MutationWebhook))

		require.Error(t, (&Adder{}).Add(mgr))
	})
}

// TestReconcileWithoutDataSync covers the mutation-webhook-only pod: process
// exclusions from the Config resource still land, with no cache manager and no
// constraint client behind it.
func TestReconcileWithoutDataSync(t *testing.T) {
	t.Cleanup(operations.AssignForTest(operations.MutationWebhook))

	ctx := context.Background()
	mgr, _ := setupManager(t)

	// Read and write directly: this test drives Reconcile by hand rather than
	// starting the manager.
	c, err := client.New(cfg, client.Options{Scheme: mgr.GetScheme()})
	require.NoError(t, err)

	instance := &configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: "gatekeeper-system",
		},
		Spec: configv1alpha1.ConfigSpec{
			Sync: configv1alpha1.Sync{
				SyncOnly: []configv1alpha1.SyncOnlyEntry{
					{Group: "", Version: "v1", Kind: "Pod"},
				},
			},
			Match: []configv1alpha1.MatchEntry{
				{
					ExcludedNamespaces: []wildcard.Wildcard{"excluded-ns"},
					Processes:          []string{"mutation-webhook"},
				},
			},
		},
	}
	require.NoError(t, c.Create(ctx, instance))
	t.Cleanup(func() { require.NoError(t, client.IgnoreNotFound(c.Delete(ctx, instance))) })

	tracker, err := readiness.SetupTrackerNoReadyz(mgr, false, false, false)
	require.NoError(t, err)

	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)
	processExcluder := process.New()

	rec, err := newReconciler(mgr, nil, processExcluder, tracker, func(context.Context) (*v1.Pod, error) { return pod, nil }, nil)
	require.NoError(t, err)
	rec.reader = c
	rec.writer = c
	rec.statusClient = c

	_, err = rec.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{
		Namespace: instance.GetNamespace(),
		Name:      instance.GetName(),
	}})
	require.NoError(t, err)

	require.Equal(t, []string{"excluded-ns"}, processExcluder.GetExcludedNamespaces(process.Mutation),
		"mutation webhook exclusions should be applied without a cache manager")

	// ConfigPodStatus is still reported for this operation set.
	statusName, err := statusv1beta1.KeyForConfig(pod.Name, instance.GetNamespace(), instance.GetName())
	require.NoError(t, err)
	status := &statusv1beta1.ConfigPodStatus{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: statusName}, status))
	require.Empty(t, status.Status.Errors)
	t.Cleanup(func() { require.NoError(t, client.IgnoreNotFound(c.Delete(ctx, status))) })
}
