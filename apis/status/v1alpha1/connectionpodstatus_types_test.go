package v1alpha1_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/v3/apis/status/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestNewConnectionStatusForPod(t *testing.T) {
	const podName = "some-gk-pod"
	const podNS = "a-gk-namespace"
	const connectionName = "audit"
	const connectionNamespace = "a-gk-ns"

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

	expectedStatus := &v1alpha1.ConnectionPodStatus{}
	expectedStatus.SetName("some--gk--pod-a--gk--ns-audit")
	expectedStatus.SetNamespace(podNS)
	expectedStatus.Status.ID = podName
	expectedStatus.Status.Operations = operations.AssignedStringList()
	expectedStatus.SetLabels(map[string]string{
		v1beta1.ConnectionNameLabel: connectionName,
		v1beta1.PodLabel:            podName,
	})

	err = controllerutil.SetOwnerReference(pod, expectedStatus, scheme)
	if err != nil {
		t.Fatal(err)
	}

	status, err := v1alpha1.NewConnectionStatusForPod(pod, connectionNamespace, connectionName, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(expectedStatus, status); diff != "" {
		t.Fatal(diff)
	}
	n, err := v1alpha1.KeyForConnection(podName, connectionNamespace, connectionName)
	if err != nil {
		t.Fatal(err)
	}
	if status.Name != n {
		t.Fatal("got status.Name != n, want equal")
	}
}

func TestConnectionPodStatusDeepCopyPublishStatuses(t *testing.T) {
	attemptTime := metav1.NewTime(time.Unix(1, 0))
	successTime := metav1.NewTime(time.Unix(2, 0))
	status := &v1alpha1.ConnectionPodStatus{
		Status: v1alpha1.ConnectionPodStatusStatus{
			PublishStatuses: []v1alpha1.ConnectionPublishStatus{
				{
					Source:          v1alpha1.WebhookPublishSource,
					Active:          true,
					LastAttemptTime: &attemptTime,
					LastSuccessTime: &successTime,
					Errors: []*v1alpha1.ConnectionError{
						{Type: v1alpha1.PublishError, Message: "original"},
					},
				},
			},
		},
	}

	copy := status.DeepCopy()
	copy.Status.PublishStatuses[0].Errors[0].Message = "changed"
	copy.Status.PublishStatuses[0].LastAttemptTime.Time = time.Unix(3, 0)

	if diff := cmp.Diff("original", status.Status.PublishStatuses[0].Errors[0].Message); diff != "" {
		t.Fatal(diff)
	}
	if diff := cmp.Diff(attemptTime, *status.Status.PublishStatuses[0].LastAttemptTime); diff != "" {
		t.Fatal(diff)
	}
}
