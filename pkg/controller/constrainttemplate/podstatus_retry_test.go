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
	"context"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// staleClient wraps a controller-runtime client and serves Get requests from a snapshot taken at construction time,
// while forwarding Update/Create/Delete/List to the real (live) client. This simulates a controller-runtime informer
// cache that has not yet observed the latest write to the backing store, which is the underlying cause of the spurious
// "the object has been modified" conflicts we hit in the ConstraintTemplate reconciler.
type staleClient struct {
	client.Client
	stale map[client.ObjectKey]*statusv1beta1.ConstraintTemplatePodStatus
}

func (s *staleClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if cts, ok := obj.(*statusv1beta1.ConstraintTemplatePodStatus); ok {
		if cached, ok := s.stale[key]; ok {
			cached.DeepCopyInto(cts)
			return nil
		}
	}
	return s.Client.Get(ctx, key, obj, opts...)
}

func newPodStatusForTest(name string) *statusv1beta1.ConstraintTemplatePodStatus {
	return &statusv1beta1.ConstraintTemplatePodStatus{
		TypeMeta: metav1.TypeMeta{
			APIVersion: statusv1beta1.GroupVersion.String(),
			Kind:       "ConstraintTemplatePodStatus",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "gatekeeper-system",
		},
	}
}

func newSchemeForTest(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, statusv1beta1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))
	return scheme
}

// TestUpdatePodStatusWithRetry_StaleCacheRecovers reproduces the race that produced "update ct pod status error" in
// production: the reconciler's cached client returns a stale resourceVersion for the PodStatus, so the first Update
// returns a 409 Conflict; the helper must refetch via the uncached APIReader and successfully retry.
func TestUpdatePodStatusWithRetry_StaleCacheRecovers(t *testing.T) {
	ctx := context.Background()
	scheme := newSchemeForTest(t)

	initial := newPodStatusForTest("pod-template-status")
	initial.Status.ID = "initial"

	// Live store: the API-server-equivalent that holds the latest resourceVersion.
	live := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(initial.DeepCopy()).
		Build()

	// Capture the resourceVersion the fake client assigned to the initial object on Build(). We can't read it off
	// `initial` because WithObjects deep-copies its input before stamping a resourceVersion on the stored copy.
	storedBefore := &statusv1beta1.ConstraintTemplatePodStatus{}
	require.NoError(t, live.Get(ctx, client.ObjectKeyFromObject(initial), storedBefore))
	staleResourceVersion := storedBefore.ResourceVersion
	require.NotEmpty(t, staleResourceVersion, "test sanity: fake client must stamp a resourceVersion on stored objects")

	// Simulate an out-of-band update advancing the resourceVersion on the API server while the controller's informer
	// cache still serves the old version.
	latestOnAPIServer := storedBefore.DeepCopy()
	latestOnAPIServer.Status.ID = "out-of-band"
	require.NoError(t, live.Update(ctx, latestOnAPIServer))
	require.NotEqual(t, staleResourceVersion, latestOnAPIServer.ResourceVersion, "test sanity: Update must bump resourceVersion")

	// The stale informer view: same spec the cache observed at storedBefore, pinned to the older resourceVersion.
	cachedView := storedBefore.DeepCopy()
	cachedView.ResourceVersion = staleResourceVersion

	cached := &staleClient{
		Client: live,
		stale: map[client.ObjectKey]*statusv1beta1.ConstraintTemplatePodStatus{
			client.ObjectKeyFromObject(initial): cachedView,
		},
	}

	r := &ReconcileConstraintTemplate{
		Client:    cached,
		apiReader: live,
		scheme:    scheme,
	}

	// The reconciler reads the PodStatus through the cached client and gets the stale version, then mutates and tries
	// to write. The first Update must fail with Conflict (server side rejects the old resourceVersion); the helper must
	// recover via the uncached apiReader.
	status := &statusv1beta1.ConstraintTemplatePodStatus{}
	require.NoError(t, cached.Get(ctx, client.ObjectKeyFromObject(initial), status))
	require.Equal(t, staleResourceVersion, status.ResourceVersion, "test sanity: cached view is pinned to the older resourceVersion")
	require.Equal(t, "initial", status.Status.ID, "test sanity: cached view still reflects the pre-out-of-band spec")
	status.Status.ID = "desired-by-reconciler"

	require.NoError(t, r.updatePodStatusWithRetry(ctx, status))

	final := &statusv1beta1.ConstraintTemplatePodStatus{}
	require.NoError(t, live.Get(ctx, client.ObjectKeyFromObject(initial), final))
	require.Equal(t, "desired-by-reconciler", final.Status.ID, "desired status must be applied on top of the latest resourceVersion")
}

// TestUpdatePodStatusWithRetry_NonConflictErrorReturned ensures that errors other than 409 Conflict are returned to the
// caller unchanged and not silently swallowed by the retry loop.
func TestUpdatePodStatusWithRetry_NonConflictErrorReturned(t *testing.T) {
	ctx := context.Background()
	scheme := newSchemeForTest(t)

	live := fake.NewClientBuilder().WithScheme(scheme).Build()

	r := &ReconcileConstraintTemplate{
		Client:    live,
		apiReader: live,
		scheme:    scheme,
	}

	// PodStatus does not exist in the live store, so Update must fail with NotFound. The helper must surface that error
	// without retrying.
	missing := newPodStatusForTest("missing-status")
	missing.Status.ID = "anything"

	err := r.updatePodStatusWithRetry(ctx, missing)
	require.Error(t, err)
	require.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
}

// TestUpdatePodStatusWithRetry_HappyPath ensures that a normal update with the latest resourceVersion succeeds without
// any retry overhead.
func TestUpdatePodStatusWithRetry_HappyPath(t *testing.T) {
	ctx := context.Background()
	scheme := newSchemeForTest(t)

	initial := newPodStatusForTest("happy-status")
	initial.Status.ID = "before"

	live := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(initial.DeepCopy()).
		Build()

	r := &ReconcileConstraintTemplate{
		Client:    live,
		apiReader: live,
		scheme:    scheme,
	}

	current := &statusv1beta1.ConstraintTemplatePodStatus{}
	require.NoError(t, live.Get(ctx, client.ObjectKeyFromObject(initial), current))
	current.Status.ID = "after"

	require.NoError(t, r.updatePodStatusWithRetry(ctx, current))

	final := &statusv1beta1.ConstraintTemplatePodStatus{}
	require.NoError(t, live.Get(ctx, types.NamespacedName{Name: initial.Name, Namespace: initial.Namespace}, final))
	require.Equal(t, "after", final.Status.ID)
}
