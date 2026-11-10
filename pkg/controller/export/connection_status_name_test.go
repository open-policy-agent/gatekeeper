package export

import (
	"context"
	"strings"
	"testing"

	connectionv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/connection/v1alpha1"
	statusv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1alpha1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	anythingtypes "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestConnectionPodStatusNameLifecycle(t *testing.T) {
	ctx := context.Background()
	namespace := "n" + strings.Repeat("-", 61) + "n"
	t.Setenv("POD_NAMESPACE", namespace)
	k8sClient, err := client.New(cfg, client.Options{Scheme: k8sscheme.Scheme})
	require.NoError(t, err)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	require.NoError(t, k8sClient.Create(ctx, ns))
	t.Cleanup(func() { require.NoError(t, k8sClient.Delete(ctx, ns)) })

	for _, tt := range []struct {
		name       string
		pod        string
		connection string
	}{
		{name: "253 byte legacy key", pod: "p-" + strings.Repeat("p", 61), connection: strings.Repeat("c", 63)},
		{name: "254 byte legacy key", pod: "p--" + strings.Repeat("p", 60), connection: strings.Repeat("c", 63)},
		{name: "maximum dash expansion", pod: "p" + strings.Repeat("-", 61) + "p", connection: "c" + strings.Repeat("-", 61) + "c"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: tt.pod, Namespace: namespace},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "manager", Image: "unused"}}},
			}
			require.NoError(t, k8sClient.Create(ctx, pod))
			t.Cleanup(func() { require.NoError(t, k8sClient.Delete(ctx, pod)) })
			getPod := func(context.Context) (*corev1.Pod, error) { return pod, nil }

			var statuses []types.NamespacedName
			for _, name := range []string{tt.connection, tt.connection[:62] + "d"} {
				conn := &connectionv1alpha1.Connection{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
					Spec: connectionv1alpha1.ConnectionSpec{
						Driver: "disk",
						Config: &anythingtypes.Anything{Value: map[string]interface{}{"path": "unused"}},
					},
				}
				require.NoError(t, k8sClient.Create(ctx, conn))
				t.Cleanup(func() { require.NoError(t, k8sClient.Delete(ctx, conn)) })

				require.NoError(t, updateOrCreateConnectionPodStatus(ctx, k8sClient, k8sClient, k8sscheme.Scheme, conn, nil, nil, getPod))
				t.Cleanup(func() { require.NoError(t, deleteStatus(ctx, k8sClient, namespace, name, getPod)) })
				key, err := statusv1alpha1.KeyForConnection(pod.Name, namespace, name)
				require.NoError(t, err)
				require.LessOrEqual(t, len(key), validation.DNS1123SubdomainMaxLength)
				statusKey := types.NamespacedName{Namespace: namespace, Name: key}
				statuses = append(statuses, statusKey)
				status := &statusv1alpha1.ConnectionPodStatus{}
				require.NoError(t, k8sClient.Get(ctx, statusKey, status))
				require.Equal(t, name, status.Labels[statusv1beta1.ConnectionNameLabel])
				require.Equal(t, pod.Name, status.Labels[statusv1beta1.PodLabel])
				require.Equal(t, pod.Name, status.Status.ID)
				require.Equal(t, conn.UID, status.Status.ConnectionUID)
				require.Len(t, status.OwnerReferences, 1)
				require.Equal(t, pod.UID, status.OwnerReferences[0].UID)
				uid := status.UID

				connectionErrors := []*statusv1alpha1.ConnectionError{{Type: statusv1alpha1.UpsertConnectionError, Message: "updated"}}
				require.NoError(t, updateOrCreateConnectionPodStatus(ctx, k8sClient, k8sClient, k8sscheme.Scheme, conn, connectionErrors, nil, getPod))
				require.NoError(t, k8sClient.Get(ctx, statusKey, status))
				require.Equal(t, uid, status.UID, "updates must find the original status object")
				require.Equal(t, connectionErrors, status.Status.ConnectionErrors)
			}

			require.NotEqual(t, statuses[0], statuses[1], "distinct Connections must not share a status")
			list := &statusv1alpha1.ConnectionPodStatusList{}
			require.NoError(t, k8sClient.List(ctx, list, client.InNamespace(namespace), client.MatchingLabels{statusv1beta1.PodLabel: pod.Name}))
			require.Len(t, list.Items, 2)

			require.NoError(t, deleteStatus(ctx, k8sClient, namespace, tt.connection, getPod))
			require.True(t, apierrors.IsNotFound(k8sClient.Get(ctx, statuses[0], &statusv1alpha1.ConnectionPodStatus{})))
			require.NoError(t, k8sClient.Get(ctx, statuses[1], &statusv1alpha1.ConnectionPodStatus{}), "deleting one status must preserve the other")
			require.NoError(t, deleteStatus(ctx, k8sClient, namespace, tt.connection[:62]+"d", getPod))
			require.NoError(t, k8sClient.List(ctx, list, client.InNamespace(namespace), client.MatchingLabels{statusv1beta1.PodLabel: pod.Name}))
			require.Empty(t, list.Items, "deleting Connections must not leave hashed statuses behind")
		})
	}
}
