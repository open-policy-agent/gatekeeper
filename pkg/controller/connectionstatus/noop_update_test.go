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

package connectionstatus

import (
	"context"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/apis"
	connectionv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/connection/v1alpha1"
	statusv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1alpha1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/disk"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const connectionConfigPathKey = "path"

func TestReconcileSkipsSecondIdenticalStatusUpdate(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)

	conn := &connectionv1alpha1.Connection{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "audit-connection",
			Namespace: util.GetNamespace(),
			UID:       k8stypes.UID("connection-uid"),
		},
		Spec: connectionv1alpha1.ConnectionSpec{
			Driver: disk.Name,
			Config: &types.Anything{Value: map[string]interface{}{connectionConfigPathKey: "value"}},
		},
	}
	podStatus := &statusv1alpha1.ConnectionPodStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-a-connection",
			Namespace: util.GetNamespace(),
			Labels: map[string]string{
				statusv1beta1.ConnectionNameLabel: conn.Name,
			},
		},
		Status: statusv1alpha1.ConnectionPodStatusStatus{
			ID:            "pod-a",
			ConnectionUID: conn.UID,
		},
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithIndex(&statusv1alpha1.ConnectionPodStatus{}, statusv1beta1.ConnectionNameLabel, indexObjectByLabel(statusv1beta1.ConnectionNameLabel)).WithObjects(conn, podStatus).Build()
	statusClient := &recordingStatusClient{client: k8sClient}
	reconciler := &ReconcileConnectionStatus{
		reader:       k8sClient,
		statusClient: statusClient,
	}
	request := reconcile.Request{NamespacedName: k8stypes.NamespacedName{Name: conn.Name, Namespace: conn.Namespace}}

	_, err := reconciler.Reconcile(ctx, request)
	require.NoError(t, err)
	require.Equal(t, 1, statusClient.updateCount, "first reconcile should self-heal missing aggregate status")

	updated := &connectionv1alpha1.Connection{}
	require.NoError(t, k8sClient.Get(ctx, request.NamespacedName, updated))
	require.Len(t, updated.Status.ByPod, 1)

	_, err = reconciler.Reconcile(ctx, request)
	require.NoError(t, err)
	require.Equal(t, 1, statusClient.updateCount, "second reconcile with identical byPod status should not write")
}

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, apis.AddToScheme(scheme))
	return scheme
}

func indexObjectByLabel(label string) client.IndexerFunc {
	return func(obj client.Object) []string {
		value := obj.GetLabels()[label]
		if value == "" {
			return nil
		}
		return []string{value}
	}
}

type recordingStatusClient struct {
	client      client.Client
	updateCount int
}

func (c *recordingStatusClient) Status() client.SubResourceWriter {
	return recordingStatusWriter{client: c.client, updateCount: &c.updateCount}
}

type recordingStatusWriter struct {
	client      client.Client
	updateCount *int
}

func (w recordingStatusWriter) Create(context.Context, client.Object, client.Object, ...client.SubResourceCreateOption) error {
	panic("unexpected status create")
}

func (w recordingStatusWriter) Update(ctx context.Context, obj client.Object, _ ...client.SubResourceUpdateOption) error {
	(*w.updateCount)++
	return w.client.Update(ctx, obj)
}

func (w recordingStatusWriter) Patch(context.Context, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
	panic("unexpected status patch")
}

func (w recordingStatusWriter) Apply(context.Context, runtime.ApplyConfiguration, ...client.SubResourceApplyOption) error {
	panic("unexpected status apply")
}
