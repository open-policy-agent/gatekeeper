package v1beta1_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestNewConfigStatusForPod(t *testing.T) {
	const podName = "some-gk-pod"
	const podNS = "a-gk-namespace"
	const configName = "a-config"
	const configNameSpace = "a-gk-ns"

	testutils.Setenv(t, "POD_NAMESPACE", podNS)

	scheme := runtime.NewScheme()
	err := v1beta1.AddToScheme(scheme)
	if err != nil {
		t.Fatal(err)
	}

	err = corev1.AddToScheme(scheme)
	if err != nil {
		t.Fatal(err)
	}

	pod := fakes.Pod(
		fakes.WithNamespace(podNS),
		fakes.WithName(podName),
	)

	expectedStatus := &v1beta1.ConfigPodStatus{}
	expectedStatus.SetName("some--gk--pod-a--gk--ns-a--config")
	expectedStatus.SetNamespace(podNS)
	expectedStatus.Status.ID = podName
	expectedStatus.Status.Operations = operations.AssignedStringList()
	expectedStatus.SetLabels(map[string]string{
		v1beta1.ConfigNameLabel: configName,
		v1beta1.PodLabel:        podName,
	})

	err = controllerutil.SetOwnerReference(pod, expectedStatus, scheme)
	if err != nil {
		t.Fatal(err)
	}

	status, err := v1beta1.NewConfigStatusForPod(pod, configNameSpace, configName, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(expectedStatus, status); diff != "" {
		t.Fatal(diff)
	}
	n, err := v1beta1.KeyForConfig(podName, configNameSpace, configName)
	if err != nil {
		t.Fatal(err)
	}
	if status.Name != n {
		t.Fatal("got status.Name != n, want equal")
	}
}

func TestNewConfigStatusForPod_SkipsOwnerRefInExternalMode(t *testing.T) {
	const podName = "some-gk-pod"
	const podNS = "a-gk-namespace"
	const configName = "a-config"
	const configNameSpace = "a-gk-ns"

	testutils.Setenv(t, "POD_NAMESPACE", podNS)

	// Enable skip OwnerRef mode (external mode)
	util.SetSkipPodOwnerRef(true)
	t.Cleanup(func() {
		util.SetSkipPodOwnerRef(false)
	})

	scheme := runtime.NewScheme()
	if err := v1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	pod := fakes.Pod(
		fakes.WithNamespace(podNS),
		fakes.WithName(podName),
	)

	status, err := v1beta1.NewConfigStatusForPod(pod, configNameSpace, configName, scheme)
	if err != nil {
		t.Fatal(err)
	}

	// Verify OwnerReference is NOT set
	if len(status.GetOwnerReferences()) != 0 {
		t.Errorf("Expected no OwnerReferences in external mode, got %d", len(status.GetOwnerReferences()))
	}

	// Verify all other fields are still populated correctly
	if status.Status.ID != podName {
		t.Errorf("Expected Status.ID = %q, got %q", podName, status.Status.ID)
	}

	labels := status.GetLabels()
	if labels[v1beta1.PodLabel] != podName {
		t.Errorf("Expected PodLabel = %q, got %q", podName, labels[v1beta1.PodLabel])
	}
	if labels[v1beta1.ConfigNameLabel] != configName {
		t.Errorf("Expected ConfigNameLabel = %q, got %q", configName, labels[v1beta1.ConfigNameLabel])
	}
}
