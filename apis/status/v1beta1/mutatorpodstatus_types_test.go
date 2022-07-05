package v1beta1_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/testhelpers"
	"github.com/open-policy-agent/gatekeeper/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/test/testutils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestNewMutatorStatusForPod(t *testing.T) {
	podName := "some-gk-pod-m"
	podNS := "a-gk-namespace-m"
	mutator := testhelpers.NewDummyMutator("a-mutator", "spec.value", nil)
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

	expectedStatus := &v1beta1.MutatorPodStatus{}
	expectedStatus.SetName("some--gk--pod--m-dummymutator-a--mutator")
	expectedStatus.SetNamespace(podNS)
	expectedStatus.Status.ID = podName
	expectedStatus.Status.Operations = operations.AssignedStringList()
	expectedStatus.SetLabels(
		map[string]string{
			v1beta1.MutatorNameLabel: "a-mutator",
			v1beta1.MutatorKindLabel: "DummyMutator",
			v1beta1.PodLabel:         podName,
		})

	err = controllerutil.SetOwnerReference(pod, expectedStatus, scheme)
	if err != nil {
		t.Fatal(err)
	}

	status, err := v1beta1.NewMutatorStatusForPod(pod, mutator.ID(), scheme)
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(expectedStatus, status); diff != "" {
		t.Fatal(diff)
	}
	cmVal, err := v1beta1.KeyForMutatorID(podName, mutator.ID())
	if err != nil {
		t.Fatal(err)
	}

	if status.Name != cmVal {
		t.Fatalf("got status name %q, want %q", status.Name, cmVal)
	}
}
