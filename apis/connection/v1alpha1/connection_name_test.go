package v1alpha1_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestConnectionNameValidation(t *testing.T) {
	ctx := context.Background()
	testEnv := &envtest.Environment{}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testEnv.Stop()) })

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "connection-name-validation"}}
	require.NoError(t, k8sClient.Create(ctx, namespace))
	dynamicClient, err := dynamic.NewForConfig(cfg)
	require.NoError(t, err)

	root := filepath.Join("..", "..", "..")
	chartCRD := filepath.Join(root, "manifest_staging", "charts", "gatekeeper", "crds", "connection-customresourcedefinition.yaml")
	for _, artifact := range []struct {
		name string
		path string
	}{
		{name: "helm", path: chartCRD},
		{name: "deployment", path: filepath.Join(root, "manifest_staging", "deploy", "gatekeeper.yaml")},
	} {
		t.Run(artifact.name, func(t *testing.T) {
			crd := installConnectionCRD(t, cfg, artifact.path)
			for _, version := range crd.Spec.Versions {
				if !version.Served {
					continue
				}
				t.Run(version.Name, func(t *testing.T) {
					resource := dynamicClient.Resource(schema.GroupVersionResource{
						Group: crd.Spec.Group, Version: version.Name, Resource: crd.Spec.Names.Plural,
					}).Namespace(namespace.Name)
					for _, length := range []int{62, 63, 64} {
						t.Run(fmt.Sprint(length), func(t *testing.T) {
							name := strings.Repeat("c", length)
							obj := connectionForName(version.Name, namespace.Name, name)
							created, err := resource.Create(ctx, obj, metav1.CreateOptions{})
							if err == nil {
								t.Cleanup(func() { require.NoError(t, resource.Delete(ctx, name, metav1.DeleteOptions{})) })
							}
							if length > 63 {
								requireNameTooLong(t, err)
								_, err = resource.Get(ctx, name, metav1.GetOptions{})
								require.True(t, apierrors.IsNotFound(err), "rejected Connection must not be persisted: %v", err)
								return
							}
							require.NoError(t, err)
							stored, err := resource.Get(ctx, name, metav1.GetOptions{})
							require.NoError(t, err)
							require.Equal(t, name, stored.GetName())
							require.Equal(t, created.GetUID(), stored.GetUID())
							require.NoError(t, unstructured.SetNestedField(stored.Object, "updated", "spec", "config", "path"))
							_, err = resource.Update(ctx, stored, metav1.UpdateOptions{})
							require.NoError(t, err)
							stored, err = resource.Get(ctx, name, metav1.GetOptions{})
							require.NoError(t, err)
							path, found, err := unstructured.NestedString(stored.Object, "spec", "config", "path")
							require.NoError(t, err)
							require.True(t, found)
							require.Equal(t, "updated", path)
						})
					}
				})
			}
		})
	}

	t.Run("existing alpha Connection survives schema tightening", func(t *testing.T) {
		// The generator's unpatched base reproduces the previously installable schema.
		installConnectionCRD(t, cfg, filepath.Join(root, "config", "crd", "bases", "connection.gatekeeper.sh_connections.yaml"))
		resource := dynamicClient.Resource(schema.GroupVersionResource{
			Group: "connection.gatekeeper.sh", Version: "v1alpha1", Resource: "connections",
		}).Namespace(namespace.Name)
		name := strings.Repeat("l", 64)
		obj := connectionForName("v1alpha1", namespace.Name, name)
		require.Eventually(t, func() bool {
			_, err := resource.Create(ctx, obj, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
			return err == nil
		}, 10*time.Second, 100*time.Millisecond, "unrestricted schema should accept the old name")
		created, err := resource.Create(ctx, obj, metav1.CreateOptions{})
		require.NoError(t, err)

		installConnectionCRD(t, cfg, chartCRD)
		rejected := connectionForName("v1alpha1", namespace.Name, strings.Repeat("r", 64))
		var createErr error
		require.Eventually(t, func() bool {
			_, createErr = resource.Create(ctx, rejected, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
			return apierrors.HasStatusCause(createErr, metav1.CauseTypeTooLong)
		}, 10*time.Second, 100*time.Millisecond, "restricted schema should reject new long names")
		requireNameTooLong(t, createErr)

		stored, err := resource.Get(ctx, name, metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, name, stored.GetName())
		require.Equal(t, created.GetUID(), stored.GetUID(), "tightening must not replace existing Connections")
		list, err := resource.List(ctx, metav1.ListOptions{})
		require.NoError(t, err)
		require.Len(t, list.Items, 1)
		require.Equal(t, created.GetUID(), list.Items[0].GetUID())

		require.NoError(t, unstructured.SetNestedField(stored.Object, "updated", "spec", "config", "path"))
		stored, err = resource.Update(ctx, stored, metav1.UpdateOptions{})
		require.NoError(t, err, "unchanged invalid names should be ratcheted on spec updates")
		require.NoError(t, unstructured.SetNestedSlice(stored.Object, []interface{}{
			map[string]interface{}{"id": "legacy-pod"},
		}, "status", "byPod"))
		_, err = resource.UpdateStatus(ctx, stored, metav1.UpdateOptions{})
		require.NoError(t, err, "unchanged invalid names should be ratcheted on status updates")
		stored, err = resource.Get(ctx, name, metav1.GetOptions{})
		require.NoError(t, err)
		path, found, err := unstructured.NestedString(stored.Object, "spec", "config", "path")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, "updated", path)
		byPod, found, err := unstructured.NestedSlice(stored.Object, "status", "byPod")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, []interface{}{map[string]interface{}{"id": "legacy-pod"}}, byPod)

		require.NoError(t, resource.Delete(ctx, name, metav1.DeleteOptions{}), "operators must still be able to remove the old object after migration")
		_, err = resource.Get(ctx, name, metav1.GetOptions{})
		require.True(t, apierrors.IsNotFound(err))
		_, err = resource.Create(ctx, obj, metav1.CreateOptions{})
		requireNameTooLong(t, err)
	})
}

func installConnectionCRD(t *testing.T, cfg *rest.Config, path string) *apiextensionsv1.CustomResourceDefinition {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()

	decoder := utilyaml.NewYAMLOrJSONDecoder(file, 4096)
	for {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		err := decoder.Decode(crd)
		require.NotEqual(t, io.EOF, err, "Connection CRD not found in %s", path)
		require.NoError(t, err)
		if crd.Kind != "CustomResourceDefinition" || crd.Name != "connections.connection.gatekeeper.sh" {
			continue
		}
		_, err = envtest.InstallCRDs(cfg, envtest.CRDInstallOptions{
			CRDs: []*apiextensionsv1.CustomResourceDefinition{crd},
		})
		require.NoError(t, err)
		return crd
	}
}

func connectionForName(version, namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "connection.gatekeeper.sh/" + version,
		"kind":       "Connection",
		"metadata": map[string]interface{}{
			"name": name, "namespace": namespace,
		},
		"spec": map[string]interface{}{
			"driver": "disk",
			"config": map[string]interface{}{"path": "unused"},
		},
	}}
}

func requireNameTooLong(t *testing.T, err error) {
	t.Helper()
	require.True(t, apierrors.IsInvalid(err), "expected API validation rejection, got %v", err)
	require.True(t, apierrors.HasStatusCause(err, metav1.CauseTypeTooLong), "expected a length validation error, got %v", err)
	var status apierrors.APIStatus
	require.ErrorAs(t, err, &status)
	require.Len(t, status.Status().Details.Causes, 1)
	require.Equal(t, "metadata.name", status.Status().Details.Causes[0].Field)
}
