package cachemanager

import (
	"context"
	"testing"

	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
)

var cfg *rest.Config

func TestMain(m *testing.M) {
	testutils.StartControlPlane(m, &cfg, 3)
}

// TestCacheManager_AddObject_RemoveObject tests that we can add/ remove objects in the cache.
func TestCacheManager_AddObject_RemoveObject(t *testing.T) {
	mgr, _ := testutils.SetupManager(t, cfg)
	opaClient := &fakes.FakeOpa{}

	tracker, err := readiness.SetupTracker(mgr, false, false, false)
	assert.NoError(t, err)

	processExcluder := process.Get()
	cm := NewCacheManager(opaClient, syncutil.NewMetricsCache(), tracker, processExcluder)
	ctx := context.Background()

	pod := fakes.Pod(
		fakes.WithNamespace("test-ns"),
		fakes.WithName("test-name"),
	)
	unstructuredPod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	require.NoError(t, err)

	require.NoError(t, cm.AddObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}))

	// test that pod is cache managed
	require.True(t, opaClient.HasGVK(pod.GroupVersionKind()))

	// now remove the object and verify it's removed
	require.NoError(t, cm.RemoveObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}))
	require.False(t, opaClient.HasGVK(pod.GroupVersionKind()))
}

// TestCacheManager_processExclusion makes sure that we don't add objects that are process excluded.
func TestCacheManager_processExclusion(t *testing.T) {
	mgr, _ := testutils.SetupManager(t, cfg)
	opaClient := &fakes.FakeOpa{}

	tracker, err := readiness.SetupTracker(mgr, false, false, false)
	assert.NoError(t, err)

	// exclude "test-ns-excluded" namespace
	processExcluder := process.Get()
	processExcluder.Add([]configv1alpha1.MatchEntry{
		{
			ExcludedNamespaces: []util.Wildcard{"test-ns-excluded"},
			Processes:          []string{"sync"},
		},
	})

	cm := NewCacheManager(opaClient, syncutil.NewMetricsCache(), tracker, processExcluder)
	ctx := context.Background()

	pod := fakes.Pod(
		fakes.WithNamespace("test-ns-excluded"),
		fakes.WithName("test-name"),
	)
	unstructuredPod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	require.NoError(t, err)
	require.NoError(t, cm.AddObject(ctx, &unstructured.Unstructured{Object: unstructuredPod}))

	// test that pod from excluded namespace is not cache managed
	require.False(t, opaClient.HasGVK(pod.GroupVersionKind()))
}

// TestCacheManager_errors tests that we cache manager responds to errors from the opa client.
func TestCacheManager_errors(t *testing.T) {
	mgr, _ := testutils.SetupManager(t, cfg)
	opaClient := &fakes.FakeOpa{}
	opaClient.SetErroring(true) // AddObject, RemoveObject will error out now.

	tracker, err := readiness.SetupTracker(mgr, false, false, false)
	assert.NoError(t, err)

	processExcluder := process.Get()
	cm := NewCacheManager(opaClient, syncutil.NewMetricsCache(), tracker, processExcluder)
	ctx := context.Background()

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
