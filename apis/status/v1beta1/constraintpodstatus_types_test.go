package v1beta1_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/apis/status/v1beta1"
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

	cstr := &unstructured.Unstructured{}
	cstr.SetGroupVersionKind(schema.GroupVersionKind{Group: v1beta1.ConstraintsGroup, Version: "v1beta1", Kind: cstrKind})
	cstr.SetName(cstrName)

	wantStatus := &v1beta1.ConstraintPodStatus{}
	wantStatus.SetName("some--gk--pod-aconstraintkind-a--constraint")
	wantStatus.SetNamespace(podNS)
	wantStatus.Status.ID = podName
	wantStatus.Status.Operations = operations.AssignedStringList()
	wantStatus.SetLabels(
		map[string]string{
			v1beta1.ConstraintNameLabel:         "a-constraint",
			v1beta1.ConstraintKindLabel:         "AConstraintKind",
			v1beta1.PodLabel:                    podName,
			v1beta1.ConstraintTemplateNameLabel: strings.ToLower(cstrKind),
		})

	err = controllerutil.SetOwnerReference(pod, wantStatus, scheme)
	if err != nil {
		t.Fatal(err)
	}

	gotStatus, err := v1beta1.NewConstraintStatusForPod(pod, cstr, scheme)
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(wantStatus, gotStatus); diff != "" {
		t.Fatal(diff)
	}

	cmVal, err := v1beta1.KeyForConstraint(podName, cstr)
	if err != nil {
		t.Fatal(err)
	}

	if cmVal != gotStatus.Name {
		t.Errorf("got Constraint key %q, want %q", cmVal, gotStatus.Name)
	}
}
