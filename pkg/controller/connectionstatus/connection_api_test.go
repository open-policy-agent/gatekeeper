package connectionstatus

import (
	"context"
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/apis"
	connectionv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/connection/v1alpha1"
	connectionv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/connection/v1beta1"
	statusv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1alpha1"
	anythingtypes "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestConnectionAPIVersions(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, apis.AddToScheme(scheme))
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)
	dynamicClient, err := dynamic.NewForConfig(cfg)
	require.NoError(t, err)

	crd := &apiextensionsv1.CustomResourceDefinition{}
	require.NoError(t, k8sClient.Get(ctx, client.ObjectKey{Name: "connections.connection.gatekeeper.sh"}, crd))
	storageVersions := []string{}
	for _, version := range crd.Spec.Versions {
		require.True(t, version.Served)
		if version.Storage {
			storageVersions = append(storageVersions, version.Name)
		}
	}
	require.Equal(t, []string{"v1beta1"}, storageVersions)

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	require.NoError(t, err)
	groups, err := discoveryClient.ServerGroups()
	require.NoError(t, err)
	found := false
	for _, group := range groups.Groups {
		if group.Name == connectionv1beta1.GroupVersion.Group {
			found = true
			require.Equal(t, connectionv1beta1.GroupVersion.String(), group.PreferredVersion.GroupVersion)
		}
	}
	require.True(t, found, "the Connection API group must be discoverable")

	for _, version := range []schema.GroupVersion{connectionv1alpha1.GroupVersion, connectionv1beta1.GroupVersion} {
		t.Run(version.Version, func(t *testing.T) {
			config := map[string]interface{}{
				"path":            "/violations/topics",
				"maxAuditResults": float64(3),
				"unknown": map[string]interface{}{
					"nested": []interface{}{true, nil, float64(7), map[string]interface{}{"key": "value"}},
					"empty":  map[string]interface{}{},
				},
			}
			key := client.ObjectKey{Name: "compatibility-" + version.Version, Namespace: util.GetNamespace()}
			meta := metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace, Labels: map[string]string{"connection-test": key.Name}}
			alpha := &connectionv1alpha1.Connection{
				ObjectMeta: meta,
				Spec:       connectionv1alpha1.ConnectionSpec{Driver: "disk", Config: &anythingtypes.Anything{Value: config}},
			}
			beta := &connectionv1beta1.Connection{
				ObjectMeta: meta,
				Spec:       connectionv1beta1.ConnectionSpec{Driver: "disk", Config: &anythingtypes.Anything{Value: config}},
			}
			var created client.Object = alpha
			if version == connectionv1beta1.GroupVersion {
				created = beta
			}
			require.NoError(t, k8sClient.Create(ctx, created))
			t.Cleanup(func() {
				require.NoError(t, client.IgnoreNotFound(k8sClient.Delete(ctx, created)))
			})

			require.NoError(t, k8sClient.Get(ctx, key, alpha))
			require.NoError(t, k8sClient.Get(ctx, key, beta))
			require.NotEmpty(t, created.GetUID())
			require.Equal(t, created.GetUID(), alpha.UID)
			require.Equal(t, alpha.UID, beta.UID)
			// Typed decoding does not retain TypeMeta, so inspect fresh server responses.
			for _, readVersion := range []schema.GroupVersion{connectionv1alpha1.GroupVersion, connectionv1beta1.GroupVersion} {
				response, err := dynamicClient.Resource(readVersion.WithResource("connections")).Namespace(key.Namespace).Get(ctx, key.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.Equal(t, readVersion.WithKind("Connection"), response.GroupVersionKind())
				require.Equal(t, alpha.UID, response.GetUID())
			}
			require.Equal(t, alpha.Spec.Driver, beta.Spec.Driver)
			require.Equal(t, config, alpha.Spec.Config.Value)
			require.Equal(t, config, beta.Spec.Config.Value)

			timestamp := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			beta.Status.ByPod = []statusv1alpha1.ConnectionPodStatusStatus{{
				ID:                 "audit-pod",
				ConnectionUID:      beta.UID,
				ObservedGeneration: beta.Generation,
				Operations:         []string{"audit", "status"},
				ConnectionErrors:   []*statusv1alpha1.ConnectionError{{Type: statusv1alpha1.UpsertConnectionError, Message: "configuration error"}},
				PublishStatuses: []statusv1alpha1.ConnectionPublishStatus{
					{
						Source:          statusv1alpha1.AuditPublishSource,
						Active:          true,
						LastAttemptTime: &timestamp,
						LastSuccessTime: &timestamp,
						Errors:          []*statusv1alpha1.ConnectionError{{Type: statusv1alpha1.PublishError, Message: "publish error"}},
					},
					{Source: statusv1alpha1.WebhookPublishSource},
				},
			}}
			require.NoError(t, k8sClient.Status().Update(ctx, beta))
			require.NoError(t, k8sClient.Get(ctx, key, alpha))
			require.True(t, apiequality.Semantic.DeepEqual(beta.Status.ByPod, alpha.Status.ByPod))
			require.Equal(t, config, alpha.Spec.Config.Value)

			originalUID := alpha.UID
			originalGeneration := alpha.Generation
			unknown, ok := config["unknown"].(map[string]interface{})
			require.True(t, ok)
			unknown["updated"] = []interface{}{"from-alpha", false}
			alpha.Spec.Config = &anythingtypes.Anything{Value: config}
			require.NoError(t, k8sClient.Update(ctx, alpha))
			require.NoError(t, k8sClient.Get(ctx, key, beta))
			require.Equal(t, originalUID, beta.UID)
			require.Greater(t, beta.Generation, originalGeneration)
			require.Equal(t, config, beta.Spec.Config.Value)
			require.True(t, apiequality.Semantic.DeepEqual(alpha.Status.ByPod, beta.Status.ByPod))

			alpha.Status.ByPod[0].ObservedGeneration = alpha.Generation
			alpha.Status.ByPod[0].ConnectionErrors = nil
			alpha.Status.ByPod[0].PublishStatuses[1].Active = true
			require.NoError(t, k8sClient.Status().Update(ctx, alpha))
			require.NoError(t, k8sClient.Get(ctx, key, beta))
			require.True(t, apiequality.Semantic.DeepEqual(alpha.Status.ByPod, beta.Status.ByPod))
			require.Equal(t, originalUID, beta.UID)
			require.Equal(t, config, beta.Spec.Config.Value)

			alphaList := &connectionv1alpha1.ConnectionList{}
			betaList := &connectionv1beta1.ConnectionList{}
			selector := client.MatchingLabels{"connection-test": key.Name}
			require.NoError(t, k8sClient.List(ctx, alphaList, client.InNamespace(key.Namespace), selector))
			require.NoError(t, k8sClient.List(ctx, betaList, client.InNamespace(key.Namespace), selector))
			require.Len(t, alphaList.Items, 1)
			require.Len(t, betaList.Items, 1)
			require.Equal(t, originalUID, alphaList.Items[0].UID)
			require.Equal(t, originalUID, betaList.Items[0].UID)
			require.Equal(t, config, alphaList.Items[0].Spec.Config.Value)
			require.Equal(t, config, betaList.Items[0].Spec.Config.Value)

			for _, direction := range []struct {
				watchVersion  schema.GroupVersion
				updateVersion schema.GroupVersion
			}{
				{watchVersion: connectionv1beta1.GroupVersion, updateVersion: connectionv1alpha1.GroupVersion},
				{watchVersion: connectionv1alpha1.GroupVersion, updateVersion: connectionv1beta1.GroupVersion},
			} {
				t.Run("watch-"+direction.watchVersion.Version+"-update-"+direction.updateVersion.Version, func(t *testing.T) {
					watchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
					defer cancel()
					updater := dynamicClient.Resource(direction.updateVersion.WithResource("connections")).Namespace(key.Namespace)
					current, err := updater.Get(watchCtx, key.Name, metav1.GetOptions{})
					require.NoError(t, err)

					// Starting from the current revision prevents missing the subsequent update.
					stream, err := dynamicClient.Resource(direction.watchVersion.WithResource("connections")).Namespace(key.Namespace).Watch(watchCtx, metav1.ListOptions{
						FieldSelector:   fields.OneTermEqualSelector("metadata.name", key.Name).String(),
						ResourceVersion: current.GetResourceVersion(),
					})
					require.NoError(t, err)
					defer stream.Stop()

					expectedValue := "from-" + direction.updateVersion.Version
					require.NoError(t, unstructured.SetNestedField(current.Object, expectedValue, "spec", "config", "watchValue"))
					updated, err := updater.Update(watchCtx, current, metav1.UpdateOptions{})
					require.NoError(t, err)

					select {
					case event, open := <-stream.ResultChan():
						require.True(t, open, "watch closed before delivering the update")
						require.Equal(t, watch.Modified, event.Type)
						observed, ok := event.Object.(*unstructured.Unstructured)
						require.True(t, ok, "unexpected watch object %T", event.Object)
						require.Equal(t, direction.watchVersion.WithKind("Connection"), observed.GroupVersionKind())
						require.Equal(t, originalUID, observed.GetUID())
						require.Equal(t, updated.GetResourceVersion(), observed.GetResourceVersion())
						value, found, err := unstructured.NestedString(observed.Object, "spec", "config", "watchValue")
						require.NoError(t, err)
						require.True(t, found)
						require.Equal(t, expectedValue, value)
					case <-watchCtx.Done():
						require.FailNow(t, "timed out waiting for the cross-version watch update", watchCtx.Err())
					}
				})
			}

			for _, invalidSpec := range []map[string]interface{}{
				{"driver": "disk"},
				{"config": map[string]interface{}{}},
			} {
				invalid := &unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": version.String(),
					"kind":       "Connection",
					"metadata":   map[string]interface{}{"name": key.Name + "-invalid", "namespace": key.Namespace},
					"spec":       invalidSpec,
				}}
				err := k8sClient.Create(ctx, invalid)
				require.True(t, apierrors.IsInvalid(err), "missing required fields must be rejected for %s: %v", version, err)
			}
		})
	}
}
