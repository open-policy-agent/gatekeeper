package v1beta1

import (
	"testing"

	. "github.com/onsi/gomega"
	"github.com/open-policy-agent/gatekeeper/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/testhelpers"
	"github.com/open-policy-agent/gatekeeper/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/test/testutils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestNewMutatorStatusForPod(t *testing.T) {
	g := NewGomegaWithT(t)
	podName := "some-gk-pod-m"
	podNS := "a-gk-namespace-m"
	mutator := testhelpers.NewDummyMutator("a-mutator", "spec.value", nil)
	testutils.Setenv(t, "POD_NAMESPACE", podNS)

	scheme := runtime.NewScheme()
	g.Expect(AddToScheme(scheme)).NotTo(HaveOccurred())
	g.Expect(corev1.AddToScheme(scheme)).NotTo(HaveOccurred())

	pod := fakes.Pod(
		fakes.WithNamespace(podNS),
		fakes.WithName(podName),
	)

	expectedStatus := &MutatorPodStatus{}
	expectedStatus.SetName("some--gk--pod--m-dummymutator-a--mutator")
	expectedStatus.SetNamespace(podNS)
	expectedStatus.Status.ID = podName
	expectedStatus.Status.Operations = operations.AssignedStringList()
	expectedStatus.SetLabels(
		map[string]string{
			MutatorNameLabel: "a-mutator",
			MutatorKindLabel: "DummyMutator",
			PodLabel:         podName,
		})
	g.Expect(controllerutil.SetOwnerReference(pod, expectedStatus, scheme)).NotTo(HaveOccurred())

	status, err := NewMutatorStatusForPod(pod, mutator.ID(), scheme)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status).To(Equal(expectedStatus))
	cmVal, err := KeyForMutatorID(podName, mutator.ID())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status.Name).To(Equal(cmVal))
}
