package v1beta1

import (
	"os"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/open-policy-agent/gatekeeper/pkg/operations"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestNewConstraintTemplateStatusForPod(t *testing.T) {
	g := NewGomegaWithT(t)
	podName := "some-gk-pod"
	podNS := "a-gk-namespace"
	templateName := "a-template"
	os.Setenv("POD_NAMESPACE", podNS)
	defer os.Unsetenv("POD_NAMESPACE")

	scheme := runtime.NewScheme()
	g.Expect(SchemeBuilder.AddToScheme(scheme)).NotTo(HaveOccurred())
	g.Expect(corev1.AddToScheme(scheme)).NotTo(HaveOccurred())

	pod := &corev1.Pod{}
	pod.SetName(podName)
	pod.SetNamespace(podNS)

	expectedStatus := &ConstraintTemplatePodStatus{}
	expectedStatus.SetName("some--gk--pod-a--template")
	expectedStatus.SetNamespace(podNS)
	expectedStatus.Status.ID = podName
	expectedStatus.Status.Operations = operations.AssignedStringList()
	expectedStatus.SetLabels(map[string]string{
		ConstraintTemplateNameLabel: templateName,
		PodLabel:                    podName,
	})
	g.Expect(controllerutil.SetOwnerReference(pod, expectedStatus, scheme)).NotTo(HaveOccurred())

	status, err := NewConstraintTemplateStatusForPod(pod, templateName, scheme)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status).To(Equal(expectedStatus))
	n, err := KeyForConstraintTemplate(podName, templateName)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(status.Name).To(Equal(n))
}
