package v1beta1

import (
	"os"
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
	SchemeBuilder.AddToScheme(scheme)
	corev1.AddToScheme(scheme)

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
	expectedStatus.SetLabels(map[string]string{ConstraintMapLabel: "AConstraintKind-a--constraint"})
	controllerutil.SetOwnerReference(pod, expectedStatus, scheme)

	status, err := NewConstraintStatusForPod(pod, cstr, scheme)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status).To(Equal(expectedStatus))
	g.Expect(status.Name).To(Equal(KeyForConstraint(podName, cstr)))
	g.Expect(DecodeConstraintLabel(status.GetLabels()[ConstraintMapLabel])).To(Equal([]string{cstrKind, cstrName}))
}
