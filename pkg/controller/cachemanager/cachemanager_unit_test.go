package cachemanager

import (
	"testing"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// TestCacheManager_AddObject_RemoveObject tests that we can add/ remove objects in the cache.
func TestCacheManager_AddObject_RemoveObject(t *testing.T) {
	cm, _, ctx := makeCacheManagerForTest(t, false, false)

	pod := fakes.Pod(
		fakes.WithNamespace("test-ns"),
		fakes.WithName("test-name"),
	)
	unstructuredPod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	require.NoError(t, err)

	require.NoError(t, cm.AddObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}))

	// test that pod is cache managed
	opaClient, ok := cm.opa.(*fakes.FakeOpa)
	require.True(t, ok)
	require.True(t, opaClient.HasGVK(pod.GroupVersionKind()))

	// now remove the object and verify it's removed
	require.NoError(t, cm.RemoveObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}))
	require.False(t, opaClient.HasGVK(pod.GroupVersionKind()))
}

// TestCacheManager_processExclusion makes sure that we don't add objects that are process excluded.
func TestCacheManager_processExclusion(t *testing.T) {
	cm, _, ctx := makeCacheManagerForTest(t, false, false)
	processExcluder := process.Get()
	processExcluder.Add([]configv1alpha1.MatchEntry{
		{
			ExcludedNamespaces: []wildcard.Wildcard{"test-ns-excluded"},
			Processes:          []string{"sync"},
		},
	})
	cm.processExcluder.Replace(processExcluder)

	pod := fakes.Pod(
		fakes.WithNamespace("test-ns-excluded"),
		fakes.WithName("test-name"),
	)
	unstructuredPod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	require.NoError(t, err)
	require.NoError(t, cm.AddObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}))

	// test that pod from excluded namespace is not cache managed
	opaClient, ok := cm.opa.(*fakes.FakeOpa)
	require.True(t, ok)
	require.False(t, opaClient.HasGVK(pod.GroupVersionKind()))
}

// TestCacheManager_errors tests that the cache manager responds to errors from the opa client.
func TestCacheManager_errors(t *testing.T) {
	cm, _, ctx := makeCacheManagerForTest(t, false, false)
	opaClient, ok := cm.opa.(*fakes.FakeOpa)
	require.True(t, ok)
	opaClient.SetErroring(true) // This will cause AddObject, RemoveObject to err

	pod := fakes.Pod(
		fakes.WithNamespace("test-ns"),
		fakes.WithName("test-name"),
	)
	unstructuredPod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	require.NoError(t, err)

	// test that cm bubbles up the errors
	require.ErrorContains(t, cm.AddObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}), "test error")
	require.ErrorContains(t, cm.RemoveObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}), "test error")
}
