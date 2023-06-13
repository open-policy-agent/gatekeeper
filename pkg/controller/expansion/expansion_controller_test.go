package expansion

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/v3/apis/expansion/v1alpha1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
)

var cfg *rest.Config

func TestMain(m *testing.M) {
	testutils.StartControlPlane(m, &cfg, 3)
}

func TestReconcile(t *testing.T) {
	// Uncommenting the below enables logging of K8s internals like watch.
	// fs := flag.NewFlagSet("", flag.PanicOnError)
	// klog.InitFlags(fs)
	// fs.Parse([]string{"--alsologtostderr", "-v=10"})
	// klog.SetOutput(os.Stderr)

	mgr, _ := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	// creating the gatekeeper-system namespace is necessary because that's where
	// status resources live by default
	err := testutils.CreateGatekeeperNamespace(mgr.GetConfig())
	if err != nil {
		t.Fatal(err)
	}

	mutSystem := mutation.NewSystem(mutation.SystemOpts{})
	expSystem := expansion.NewSystem(mutSystem)

	testutils.Setenv(t, "POD_NAME", "no-pod")

	tracker, err := readiness.SetupTracker(mgr, false, false, true)
	if err != nil {
		t.Fatal(err)
	}

	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	r := newReconciler(mgr, expSystem, func(context.Context) (*corev1.Pod, error) { return pod, nil }, tracker)
	if err != nil {
		t.Fatal(err)
	}

	err = add(mgr, r)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	t.Run("creating ET creates status obj, deleting ET deletes status", func(t *testing.T) {
		t.Log("running test: creating ET creates ETPodStatus, deleting ET deletes status")

		etName := "default-et"
		et := newET(etName)

		sName, err := statusv1beta1.KeyForExpansionTemplate("no-pod", etName)
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, et))
		testutils.CreateThenCleanup(ctx, t, c, et)

		err = retry.OnError(testutils.ConstantRetry, func(err error) bool {
			return true
		}, func() error {
			// First, get the ET
			et := &v1alpha1.ExpansionTemplate{}
			nsName := types.NamespacedName{Name: etName}
			if err := c.Get(ctx, nsName, et); err != nil {
				return err
			}
			if err != nil {
				return fmt.Errorf("error fetching ET: %w", err)
			}

			// Get the ETPodStatus
			status := &statusv1beta1.ExpansionTemplatePodStatus{}
			nsName = types.NamespacedName{
				Name:      sName,
				Namespace: "gatekeeper-system",
			}
			if err := c.Get(ctx, nsName, status); err != nil {
				return err
			}
			if err != nil {
				return fmt.Errorf("error fetching ET status: %w", err)
			}
			if status.Status.TemplateUID == et.GetUID() {
				return nil
			}
			return fmt.Errorf("ExpansionTemplatePodStatus.Status.TemplateUID %q does not match ExpansionTemplate.GetUID() %q", status.Status.TemplateUID, et.GetUID())
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := c.Delete(ctx, et); err != nil {
			t.Fatalf("error deleting ET: %s", err)
		}

		err = retry.OnError(testutils.ConstantRetry, func(err error) bool {
			return true
		}, func() error {
			// Get the ETPodStatus
			status := &statusv1beta1.ExpansionTemplatePodStatus{}
			nsName := types.NamespacedName{
				Name:      sName,
				Namespace: "gatekeeper-system",
			}
			if err := c.Get(ctx, nsName, status); err != nil && apierrors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("expected IsNotFound when fetching status, but got: %w", err)
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestAddStatusError(t *testing.T) {
	tests := []struct {
		name        string
		inputStatus statusv1beta1.ExpansionTemplatePodStatus
		etErr       error
		wantStatus  statusv1beta1.ExpansionTemplatePodStatus
	}{
		{
			name:        "no err",
			inputStatus: statusv1beta1.ExpansionTemplatePodStatus{},
			etErr:       nil,
			wantStatus:  statusv1beta1.ExpansionTemplatePodStatus{Status: statusv1beta1.ExpansionTemplatePodStatusStatus{}},
		},
		{
			name:        "with err",
			inputStatus: statusv1beta1.ExpansionTemplatePodStatus{},
			etErr:       errors.New("big problem"),
			wantStatus: statusv1beta1.ExpansionTemplatePodStatus{
				Status: statusv1beta1.ExpansionTemplatePodStatusStatus{
					Errors: []*statusv1beta1.ExpansionTemplateError{{Message: "big problem"}},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setStatusError(&tc.inputStatus, tc.etErr)
			if diff := cmp.Diff(tc.inputStatus, tc.wantStatus); diff != "" {
				t.Errorf("got: %v\nwant: %v\ndiff: %s", tc.inputStatus, tc.wantStatus, diff)
			}
		})
	}
}

func newET(name string) *v1alpha1.ExpansionTemplate {
	et := &v1alpha1.ExpansionTemplate{
		ObjectMeta: v1.ObjectMeta{Name: name},
		Spec: v1alpha1.ExpansionTemplateSpec{
			ApplyTo: []match.ApplyTo{{
				Groups:   []string{"apps"},
				Kinds:    []string{"Deployment"},
				Versions: []string{"v1"},
			}},
			TemplateSource: "spec.template",
			GeneratedGVK: v1alpha1.GeneratedGVK{
				Kind:    "Pod",
				Version: "v1",
			},
		},
	}
	et.SetGroupVersionKind(v1alpha1.GroupVersion.WithKind("ExpansionTemplate"))
	return et
}
