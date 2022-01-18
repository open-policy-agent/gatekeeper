package v1beta1

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/test/testutils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestNewConstraintStatusForPod(t *testing.T) {
	podName := "some-gk-pod"
	podNS := "a-gk-namespace"
	cstrName := "a-constraint"
	cstrKind := "AConstraintKind"
	testutils.Setenv(t, "POD_NAMESPACE", podNS)

	scheme := runtime.NewScheme()
	err := AddToScheme(scheme)
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

	cstr := &unstructured.Unstructured{}
	cstr.SetGroupVersionKind(schema.GroupVersionKind{Group: ConstraintsGroup, Version: "v1beta1", Kind: cstrKind})
	cstr.SetName(cstrName)

	wantStatus := &ConstraintPodStatus{}
	wantStatus.SetName("some--gk--pod-aconstraintkind-a--constraint")
	wantStatus.SetNamespace(podNS)
	wantStatus.Status.ID = podName
	wantStatus.Status.Operations = operations.AssignedStringList()
	wantStatus.SetLabels(
		map[string]string{
			ConstraintNameLabel:         "a-constraint",
			ConstraintKindLabel:         "AConstraintKind",
			PodLabel:                    podName,
			ConstraintTemplateNameLabel: strings.ToLower(cstrKind),
		})

	err = controllerutil.SetOwnerReference(pod, wantStatus, scheme)
	if err != nil {
		t.Fatal(err)
	}

	gotStatus, err := NewConstraintStatusForPod(pod, cstr, scheme)
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(wantStatus, gotStatus); diff != "" {
		t.Fatal(diff)
	}

	cmVal, err := KeyForConstraint(podName, cstr)
	if err != nil {
		t.Fatal(err)
	}

	if cmVal != gotStatus.Name {
		t.Errorf("got Constraint key %q, want %q", cmVal, gotStatus.Name)
	}
}
