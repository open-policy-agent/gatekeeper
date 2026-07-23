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

	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestGetOrCreatePodStatus_PersistsInitialStatus(t *testing.T) {
	ctx := t.Context()
	testutils.Setenv(t, "POD_NAME", "no-pod")

	mgr, _ := testutils.SetupManager(t, cfg)
	require.NoError(t, testutils.CreateGatekeeperNamespace(mgr.GetConfig()))

	c := mgr.GetClient()
	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)
	require.NoError(t, c.Create(ctx, pod))
	t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, pod))

	r := &ReconcileConstraintTemplate{
		Client: c,
		scheme: mgr.GetScheme(),
		getPod: func(context.Context) (*corev1.Pod, error) { return pod, nil },
	}

	const ctName = "persist-initial-status"
	status, err := r.getOrCreatePodStatus(ctx, ctName)
	require.NoError(t, err)
	t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, status))

	require.Equal(t, "no-pod", status.Status.ID)
	require.Equal(t, operations.AssignedStringList(), status.Status.Operations)

	// Re-read from the API as a later reconcile would after the create path returns early.
	stored := &statusv1beta1.ConstraintTemplatePodStatus{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(status), stored))
	require.Equal(t, "no-pod", stored.Status.ID, "status subresource must persist ID after Create")
	require.Equal(t, operations.AssignedStringList(), stored.Status.Operations, "status subresource must persist Operations after Create")
}

func TestGetOrCreatePodStatus_ReturnsPersistedStatus(t *testing.T) {
	ctx := t.Context()
	testutils.Setenv(t, "POD_NAME", "no-pod")

	mgr, _ := testutils.SetupManager(t, cfg)
	require.NoError(t, testutils.CreateGatekeeperNamespace(mgr.GetConfig()))

	c := mgr.GetClient()
	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)
	require.NoError(t, c.Create(ctx, pod))
	t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, pod))

	r := &ReconcileConstraintTemplate{
		Client: c,
		scheme: mgr.GetScheme(),
		getPod: func(context.Context) (*corev1.Pod, error) { return pod, nil },
	}

	const ctName = "return-persisted-status"
	created, err := r.getOrCreatePodStatus(ctx, ctName)
	require.NoError(t, err)
	t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, created))

	fetched, err := r.getOrCreatePodStatus(ctx, ctName)
	require.NoError(t, err)
	require.Equal(t, created.Name, fetched.Name)
	require.Equal(t, "no-pod", fetched.Status.ID)
	require.Equal(t, operations.AssignedStringList(), fetched.Status.Operations)
}
