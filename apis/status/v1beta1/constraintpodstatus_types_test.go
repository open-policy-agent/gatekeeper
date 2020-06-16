package v1beta1

import (
	"os"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/open-policy-agent/gatekeeper/pkg/operations"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestNewConstraintStatusForPod(t *testing.T) {
	g := NewGomegaWithT(t)
	podName := "some-gk-pod"
	podNS := "a-gk-namespace"
	cstrName := "a-constraint"
	cstrKind := "AConstraintKind"
	os.Setenv("POD_NAMESPACE", podNS)
	defer os.Unsetenv("POD_NAMESPACE")

	scheme := runtime.NewScheme()
	g.Expect(SchemeBuilder.AddToScheme(scheme)).NotTo(HaveOccurred())
	g.Expect(corev1.AddToScheme(scheme)).NotTo(HaveOccurred())

	pod := &corev1.Pod{}
	pod.SetName(podName)
	pod.SetNamespace(podNS)

	cstr := &unstructured.Unstructured{}
	cstr.SetGroupVersionKind(schema.GroupVersionKind{Group: ConstraintsGroup, Version: "v1beta1", Kind: cstrKind})
	cstr.SetName(cstrName)

	expectedStatus := &ConstraintPodStatus{}
	expectedStatus.SetName("some--gk--pod-aconstraintkind-a--constraint")
	expectedStatus.SetNamespace(podNS)
	expectedStatus.Status.ID = podName
	expectedStatus.Status.Operations = operations.AssignedStringList()
	expectedStatus.SetLabels(
		map[string]string{
			ConstraintNameLabel:         "a-constraint",
			ConstraintKindLabel:         "AConstraintKind",
			PodLabel:                    podName,
			ConstraintTemplateNameLabel: strings.ToLower(cstrKind),
		})
	g.Expect(controllerutil.SetOwnerReference(pod, expectedStatus, scheme)).NotTo(HaveOccurred())

	status, err := NewConstraintStatusForPod(pod, cstr, scheme)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status).To(Equal(expectedStatus))
	cmVal, err := KeyForConstraint(podName, cstr)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status.Name).To(Equal(cmVal))
}
