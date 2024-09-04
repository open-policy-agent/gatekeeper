/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package constrainttemplate

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	templatesv1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/k8scel"
	celSchema "github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/k8scel/schema"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/reviews"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/constraint"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"golang.org/x/net/context"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	DenyAll        = "DenyAll"
	denyall        = "denyall"
	vapBindingName = "gatekeeper-denyallconstraint"
)

func makeReconcileConstraintTemplate(suffix string) *v1beta1.ConstraintTemplate {
	return &v1beta1.ConstraintTemplate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConstraintTemplate",
			APIVersion: templatesv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: denyall + strings.ToLower(suffix),
		},
		Spec: v1beta1.ConstraintTemplateSpec{
			CRD: v1beta1.CRD{
				Spec: v1beta1.CRDSpec{
					Names: v1beta1.Names{
						Kind: DenyAll + suffix,
					},
				},
			},
			Targets: []v1beta1.Target{
				{
					Target: target.Name,
					Rego: `
package foo

violation[{"msg": "denied!"}] {
	1 == 1
}
`,
				},
			},
		},
	}
}

func makeReconcileConstraintTemplateWithRegoEngine(suffix string) *v1beta1.ConstraintTemplate {
	return &v1beta1.ConstraintTemplate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConstraintTemplate",
			APIVersion: templatesv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: denyall + strings.ToLower(suffix),
		},
		Spec: v1beta1.ConstraintTemplateSpec{
			CRD: v1beta1.CRD{
				Spec: v1beta1.CRDSpec{
					Names: v1beta1.Names{
						Kind: DenyAll + suffix,
					},
				},
			},
			Targets: []v1beta1.Target{
				{
					Target: target.Name,
					Code: []v1beta1.Code{
						{
							Engine: "Rego",
							Source: &templates.Anything{
								Value: `
package foo

violation[{"msg": "denied!"}] {
	1 == 1
}
`,
							},
						},
					},
				},
			},
		},
	}
}

func makeReconcileConstraintTemplateForVap(suffix string, generateVAP *bool) *v1beta1.ConstraintTemplate {
	source := &celSchema.Source{
		// FailurePolicy: ptr.To[string]("Fail"),
		// TODO(ritazh): enable fail when VAP reduces 30s discovery of CRDs
		// due to discovery mechanism to pickup the change to the CRD list
		MatchConditions: []celSchema.MatchCondition{
			{
				Name:       "must_match_something",
				Expression: "true == true",
			},
		},
		Variables: []celSchema.Variable{
			{
				Name:       "my_variable",
				Expression: "true",
			},
		},
		Validations: []celSchema.Validation{
			{
				Expression:        "1 == 1",
				Message:           "some fallback message",
				MessageExpression: `"some CEL string"`,
			},
		},
		GenerateVAP: generateVAP,
	}
	return &v1beta1.ConstraintTemplate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConstraintTemplate",
			APIVersion: templatesv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: denyall + strings.ToLower(suffix),
		},
		Spec: v1beta1.ConstraintTemplateSpec{
			CRD: v1beta1.CRD{
				Spec: v1beta1.CRDSpec{
					Names: v1beta1.Names{
						Kind: DenyAll + suffix,
					},
				},
			},
			Targets: []v1beta1.Target{
				{
					Target: target.Name,
					Code: []v1beta1.Code{
						{
							Engine: "K8sNativeValidation",
							Source: &templates.Anything{
								Value: source.MustToUnstructured(),
							},
						},
					},
				},
			},
		},
	}
}

func crdKey(suffix string) types.NamespacedName {
	return types.NamespacedName{Name: fmt.Sprintf("denyall%s.constraints.gatekeeper.sh", strings.ToLower(suffix))}
}

func expectedCRD(suffix string) *apiextensions.CustomResourceDefinition {
	crd := &apiextensions.CustomResourceDefinition{}
	key := crdKey(suffix)
	crd.SetName(key.Name)
	crd.SetGroupVersionKind(apiextensionsv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"))
	return crd
}

func TestReconcile(t *testing.T) {
	// Uncommenting the below enables logging of K8s internals like watch.
	// fs := flag.NewFlagSet("", flag.PanicOnError)
	// klog.InitFlags(fs)
	// fs.Parse([]string{"--alsologtostderr", "-v=10"})
	// klog.SetOutput(os.Stderr)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	// creating the gatekeeper-system namespace is necessary because that's where
	// status resources live by default
	err := testutils.CreateGatekeeperNamespace(mgr.GetConfig())
	if err != nil {
		t.Fatal(err)
	}

	driver, err := rego.New(rego.Tracing(true))
	if err != nil {
		t.Fatalf("unable to set up Driver: %v", err)
	}
	// initialize K8sValidation
	k8sDriver, err := k8scel.New()
	if err != nil {
		t.Fatalf("unable to set up K8s native driver: %v", err)
	}

	cfClient, err := constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver), constraintclient.Driver(k8sDriver), constraintclient.EnforcementPoints(util.AuditEnforcementPoint))
	if err != nil {
		t.Fatalf("unable to set up constraint framework client: %s", err)
	}

	testutils.Setenv(t, "POD_NAME", "no-pod")

	cs := watch.NewSwitch()
	tracker, err := readiness.SetupTracker(mgr, false, false, false)
	if err != nil {
		t.Fatal(err)
	}

	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	// events will be used to receive events from dynamic watches registered
	events := make(chan event.GenericEvent, 1024)
	rec, err := newReconciler(mgr, cfClient, wm, cs, tracker, events, events, func(context.Context) (*corev1.Pod, error) { return pod, nil })
	if err != nil {
		t.Fatal(err)
	}

	err = add(mgr, rec)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	constraint.VapAPIEnabled = ptr.To[bool](true)
	constraint.GroupVersion = &admissionregistrationv1beta1.SchemeGroupVersion

	t.Run("CRD Gets Created", func(t *testing.T) {
		suffix := "CRDGetsCreated"

		logger.Info("Running test: CRD Gets Created")
		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		clientset := kubernetes.NewForConfigOrDie(cfg)
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			crd := &apiextensionsv1.CustomResourceDefinition{}
			if err := c.Get(ctx, crdKey(suffix), crd); err != nil {
				return err
			}
			rs, err := clientset.Discovery().ServerResourcesForGroupVersion("constraints.gatekeeper.sh/v1beta1")
			if err != nil {
				return err
			}
			for _, r := range rs.APIResources {
				if r.Kind == DenyAll+suffix {
					return nil
				}
			}
			return errors.New("DenyAll not found")
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Vap should be created with v1beta1", func(t *testing.T) {
		suffix := "VapShouldBeCreatedV1Beta1"

		logger.Info("Running test: Vap should be created with v1beta1")
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true))
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vap resource exists now
			vap := &admissionregistrationv1beta1.ValidatingAdmissionPolicy{}
			vapName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))
			if err := c.Get(ctx, types.NamespacedName{Name: vapName}, vap); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Vap should not be created", func(t *testing.T) {
		suffix := "VapShouldNotBeCreated"

		logger.Info("Running test: Vap should not be created")
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](false))
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vap resource exists now
			vap := &admissionregistrationv1beta1.ValidatingAdmissionPolicy{}
			vapName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))
			if err := c.Get(ctx, types.NamespacedName{Name: vapName}, vap); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}
			return fmt.Errorf("should result in error, vap not found")
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Vap should not be created for rego only template", func(t *testing.T) {
		suffix := "VapShouldNotBeCreatedForRegoOnlyTemplate"

		logger.Info("Running test: Vap should not be created")
		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vap resource exists now
			vap := &admissionregistrationv1beta1.ValidatingAdmissionPolicy{}
			vapName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))
			if err := c.Get(ctx, types.NamespacedName{Name: vapName}, vap); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}
			return fmt.Errorf("should result in error, vap not found")
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Vap should not be created for only rego engine", func(t *testing.T) {
		suffix := "VapShouldNotBeCreatedForOnlyRegoEngine"

		logger.Info("Running test: Vap should not be created")
		constraintTemplate := makeReconcileConstraintTemplateWithRegoEngine(suffix)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vap resource exists now
			vap := &admissionregistrationv1beta1.ValidatingAdmissionPolicy{}
			vapName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))
			if err := c.Get(ctx, types.NamespacedName{Name: vapName}, vap); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}
			return fmt.Errorf("should result in error, vap not found")
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Vap should not be created without generateVAP", func(t *testing.T) {
		suffix := "VapShouldNotBeCreatedWithoutGenerateVAP"

		logger.Info("Running test: Vap should not be created without generateVAP")
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, nil)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vap resource exists now
			vap := &admissionregistrationv1beta1.ValidatingAdmissionPolicy{}
			vapName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))
			if err := c.Get(ctx, types.NamespacedName{Name: vapName}, vap); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}
			return fmt.Errorf("should result in error, vap not found")
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Vap should be created without generateVAP field", func(t *testing.T) {
		suffix := "VapShouldBeCreatedWithoutGenerateVAP"
		logger.Info("Running test: Vap should be created without generateVAP field")
		constraint.DefaultGenerateVAP = ptr.To[bool](true)
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, nil)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vap resource exists now
			vap := &admissionregistrationv1beta1.ValidatingAdmissionPolicy{}
			vapName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))
			if err := c.Get(ctx, types.NamespacedName{Name: vapName}, vap); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VapBinding should not be created", func(t *testing.T) {
		suffix := "VapBindingShouldNotBeCreated"
		logger.Info("Running test: VapBinding should not be created")
		constraint.DefaultGenerateVAPB = ptr.To[bool](false)
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](false))
		cstr := newDenyAllCstr(suffix)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			return c.Create(ctx, cstr)
		})
		if err != nil {
			logger.Error(err, "create cstr")
			t.Fatal(err)
		}
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vapbinding resource exists now
			vapBinding := &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{}
			vapBindingName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))
			if err := c.Get(ctx, types.NamespacedName{Name: vapBindingName}, vapBinding); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}
			return fmt.Errorf("should result in error, vapbinding not found")
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VapBinding should not be created with missing CEL", func(t *testing.T) {
		suffix := "VapBindingShouldNotBeCreatedMissingCEL"
		logger.Info("Running test: VapBinding should not be created with missing CEL")
		constraint.DefaultGenerateVAPB = ptr.To[bool](true)
		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		cstr := newDenyAllCstr(suffix)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			return c.Create(ctx, cstr)
		})
		if err != nil {
			logger.Error(err, "create cstr")
			t.Fatal(err)
		}
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vapbinding resource exists now
			vapBinding := &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{}
			vapBindingName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))
			if err := c.Get(ctx, types.NamespacedName{Name: vapBindingName}, vapBinding); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}
			return fmt.Errorf("should result in error, vapbinding not found")
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Error should be present on constraint when VAP generation is off and VAPB generation is on for CEL templates", func(t *testing.T) {
		suffix := "ErrorShouldBePresentOnConstraint"
		logger.Info("Running test: Error should be present on constraint when VAP generation is off and VAPB generation is on")
		constraint.DefaultGenerateVAP = ptr.To[bool](false)
		constraint.DefaultGenerateVAPB = ptr.To[bool](true)
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, nil)
		cstr := newDenyAllCstr(suffix)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			return c.Create(ctx, cstr)
		})
		if err != nil {
			logger.Error(err, "create cstr")
			t.Fatal(err)
		}

		err := isConstraintStatuErrorAsExpected(ctx, c, suffix, true, "Conditions are not satisfied")
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Error should not be present on constraint when VAP generation if off and VAPB generation is on for templates without CEL", func(t *testing.T) {
		suffix := "ErrorShouldNotBePresentOnConstraint"
		logger.Info("Running test: Error should not be present on constraint when VAP generation is off and VAPB generation is on for templates wihout CEL")
		constraint.DefaultGenerateVAP = ptr.To[bool](false)
		constraint.DefaultGenerateVAPB = ptr.To[bool](true)
		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		cstr := newDenyAllCstr(suffix)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			return c.Create(ctx, cstr)
		})
		if err != nil {
			logger.Error(err, "create cstr")
			t.Fatal(err)
		}
		err := isConstraintStatuErrorAsExpected(ctx, c, suffix, false, "")
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VapBinding should not be created without generateVap intent in CT", func(t *testing.T) {
		suffix := "VapBindingShouldNotBeCreatedWithoutGenerateVapIntent"
		logger.Info("Running test: VapBinding should not be created without generateVap intent in CT")
		constraint.DefaultGenerateVAPB = ptr.To[bool](true)
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](false))
		cstr := newDenyAllCstr(suffix)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			return c.Create(ctx, cstr)
		})
		if err != nil {
			logger.Error(err, "create cstr")
			t.Fatal(err)
		}
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			vapBinding := &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{}
			vapBindingName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))
			if err := c.Get(ctx, types.NamespacedName{Name: vapBindingName}, vapBinding); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}
			return fmt.Errorf("should result in error, vapbinding not found")
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VapBinding should be created with VAP enforcement Point", func(t *testing.T) {
		suffix := "VapBindingShouldBeCreatedWithVAPEnforcementPoint"
		logger.Info("Running test: VapBinding should be created with VAP enforcement point")
		constraint.DefaultGenerateVAPB = ptr.To[bool](false)
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true))
		cstr := newDenyAllCstrWithScopedEA(suffix, util.VAPEnforcementPoint)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			return c.Create(ctx, cstr)
		})
		if err != nil {
			logger.Error(err, "create cstr")
			t.Fatal(err)
		}
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vapbinding resource exists now
			vapBinding := &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{}
			if err := c.Get(ctx, types.NamespacedName{Name: vapBindingName}, vapBinding); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VapBinding should not be created without VAP enforcement Point", func(t *testing.T) {
		suffix := "VapBShouldNotBeCreatedWithoutVAPEP"
		logger.Info("Running test: VapBinding should not be created without VAP enforcement point")
		constraint.DefaultGenerateVAPB = ptr.To[bool](true)
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true))
		cstr := newDenyAllCstrWithScopedEA(suffix, util.AuditEnforcementPoint)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			return c.Create(ctx, cstr)
		})
		if err != nil {
			logger.Error(err, "create cstr")
			t.Fatal(err)
		}
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vapbinding resource exists now
			vapBinding := &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{}
			vapBindingName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))
			if err := c.Get(ctx, types.NamespacedName{Name: vapBindingName}, vapBinding); err != nil {
				if !apierrors.IsNotFound(err) {
					return err
				}
				return nil
			}
			return fmt.Errorf("should result in error, vapbinding not found")
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Vap should be created with v1", func(t *testing.T) {
		suffix := "VapShouldBeCreatedV1"

		logger.Info("Running test: Vap should be created with v1")
		constraint.GroupVersion = &admissionregistrationv1.SchemeGroupVersion
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true))
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vap resource exists now
			vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
			vapName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))
			if err := c.Get(ctx, types.NamespacedName{Name: vapName}, vap); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VapBinding should be created with v1", func(t *testing.T) {
		suffix := "VapBindingShouldBeCreatedV1"
		logger.Info("Running test: VapBinding should be created with v1")
		constraint.DefaultGenerateVAPB = ptr.To[bool](true)
		constraint.GroupVersion = &admissionregistrationv1.SchemeGroupVersion
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true))
		cstr := newDenyAllCstr(suffix)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			return c.Create(ctx, cstr)
		})
		if err != nil {
			logger.Error(err, "create cstr")
			t.Fatal(err)
		}
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vapbinding resource exists now
			vapBinding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
			if err := c.Get(ctx, types.NamespacedName{Name: vapBindingName}, vapBinding); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Constraint is marked as enforced", func(t *testing.T) {
		suffix := "MarkedEnforced"

		logger.Info("Running test: Constraint is marked as enforced")
		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		cstr := newDenyAllCstr(suffix)

		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, cstr))
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(error) bool {
			return true
		}, func() error {
			return c.Create(ctx, cstr)
		})
		if err != nil {
			t.Fatal(err)
		}

		err = constraintEnforced(ctx, c, suffix)
		if err != nil {
			t.Fatal(err)
		}

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testns",
			},
		}
		req := admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Namespace",
			},
			Operation: "Create",
			Name:      "FooNamespace",
			Object:    runtime.RawExtension{Object: ns},
		}
		resp, err := cfClient.Review(ctx, req, reviews.EnforcementPoint(util.AuditEnforcementPoint))
		if err != nil {
			t.Fatal(err)
		}

		gotResults := resp.Results()
		if len(gotResults) != 1 {
			t.Log(resp.TraceDump())
			t.Log(cfClient.Dump(ctx))
			t.Fatalf("want 1 result, got %v", gotResults)
		}
	})

	t.Run("Constraint with scoped enforcement actions is marked as enforced", func(t *testing.T) {
		suffix := "ScopedMarkedEnforced"

		logger.Info("Running test: Constraint with scoped enforcement actions is marked as enforced")
		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		cstr := newDenyAllCstrWithScopedEA(suffix, util.AuditEnforcementPoint)

		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, cstr))
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(error) bool {
			return true
		}, func() error {
			return c.Create(ctx, cstr)
		})
		if err != nil {
			t.Fatal(err)
		}

		err = constraintEnforced(ctx, c, suffix)
		if err != nil {
			t.Fatal(err)
		}

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testns",
			},
		}
		req := admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Namespace",
			},
			Operation: "Create",
			Name:      "FooNamespace",
			Object:    runtime.RawExtension{Object: ns},
		}
		resp, err := cfClient.Review(ctx, req, reviews.EnforcementPoint(util.AuditEnforcementPoint))
		if err != nil {
			t.Fatal(err)
		}

		gotResults := resp.Results()
		if len(gotResults) != 1 {
			t.Log(resp.TraceDump())
			t.Log(cfClient.Dump(ctx))
			t.Fatalf("want 1 result, got %v", gotResults)
		}
	})

	t.Run("Constraint with different ep than client and review should not be part of the review", func(t *testing.T) {
		suffix := "ShouldNotBePartOfReview"

		logger.Info("Running test: Constraint with different ep than client and review should not be part of the review")
		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		cstr := newDenyAllCstrWithScopedEA(suffix, util.WebhookEnforcementPoint)

		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, cstr))
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(error) bool {
			return true
		}, func() error {
			return c.Create(ctx, cstr)
		})
		if err != nil {
			t.Fatal(err)
		}

		err = constraintEnforced(ctx, c, suffix)
		if err != nil {
			t.Fatal(err)
		}

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testns",
			},
		}
		req := admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Namespace",
			},
			Operation: "Create",
			Name:      "FooNamespace",
			Object:    runtime.RawExtension{Object: ns},
		}
		resp, err := cfClient.Review(ctx, req, reviews.EnforcementPoint(util.AuditEnforcementPoint))
		if err != nil {
			t.Fatal(err)
		}

		gotResults := resp.Results()
		if len(gotResults) >= 1 {
			t.Log(resp.TraceDump())
			t.Log(cfClient.Dump(ctx))
			t.Fatalf("want 0 result, got %v", gotResults)
		}
	})

	t.Run("Revew request initiated from an enforcement point not supported by client should result in error", func(t *testing.T) {
		suffix := "ReviewResultsInError"

		logger.Info("Running test: Review request initiated from an enforcement point not supported by client should result in error")
		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		cstr := newDenyAllCstrWithScopedEA(suffix, util.WebhookEnforcementPoint)

		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, cstr))
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testns",
			},
		}
		req := admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Namespace",
			},
			Operation: "Create",
			Name:      "FooNamespace",
			Object:    runtime.RawExtension{Object: ns},
		}
		_, err := cfClient.Review(ctx, req, reviews.EnforcementPoint(util.WebhookEnforcementPoint))
		if err == nil {
			t.Fatal("want error enforcement point is not supported by client, got nil")
		}
	})

	t.Run("Constraint With invalid enforcement point should get error in status", func(t *testing.T) {
		suffix := "InvalidEnforcementPoint"

		logger.Info("Running test: Constraint With invalid enforcement point should get error in status")
		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		cstr := newDenyAllCstrWithScopedEA(suffix, "invalid")
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, cstr))
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(error) bool {
			return true
		}, func() error {
			return c.Create(ctx, cstr)
		})
		if err != nil {
			t.Fatal(err)
		}

		err = retry.OnError(testutils.ConstantRetry, func(error) bool {
			return true
		}, func() error {
			err := c.Get(ctx, types.NamespacedName{Name: "denyallconstraint"}, cstr)
			if err != nil {
				return err
			}
			status, err := getCByPodStatus(cstr)
			if err != nil {
				return err
			}

			errorReported := false
			for _, e := range status.Errors {
				if strings.Contains(e.Message, "unrecognized enforcement points") {
					errorReported = true
					break
				}
			}
			if !errorReported {
				return fmt.Errorf("want error in constraint status, got %+v", status)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Deleted constraint CRDs are recreated", func(t *testing.T) {
		suffix := "CRDRecreated"

		logger.Info("Running test: Deleted constraint CRDs are recreated")
		// Clean up to remove the crd, constraint and constraint template
		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		cstr := newDenyAllCstr(suffix)

		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, cstr))
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		var crd *apiextensionsv1.CustomResourceDefinition
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			crd = &apiextensionsv1.CustomResourceDefinition{}
			return c.Get(ctx, crdKey(suffix), crd)
		})
		if err != nil {
			t.Fatal(err)
		}

		origUID := crd.GetUID()
		crd.Spec = apiextensionsv1.CustomResourceDefinitionSpec{}
		err = c.Delete(ctx, crd)
		if err != nil {
			t.Fatal(err)
		}

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			crd := &apiextensionsv1.CustomResourceDefinition{}
			if err := c.Get(ctx, crdKey(suffix), crd); err != nil {
				return err
			}
			if !crd.GetDeletionTimestamp().IsZero() {
				return errors.New("still deleting")
			}
			if crd.GetUID() == origUID {
				return errors.New("not yet deleted")
			}
			for _, cond := range crd.Status.Conditions {
				if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
					return nil
				}
			}
			return errors.New("not established")
		})
		if err != nil {
			t.Fatal(err)
		}

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			sList := &statusv1beta1.ConstraintPodStatusList{}
			if err := c.List(ctx, sList); err != nil {
				return err
			}
			if len(sList.Items) != 0 {
				return fmt.Errorf("remaining status items: %+v", sList.Items)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			return c.Create(ctx, newDenyAllCstr(suffix))
		})
		if err != nil {
			t.Fatal(err)
		}

		// we need a longer timeout because deleting the CRD interrupts the watch
		err = constraintEnforced(ctx, c, suffix)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Templates with Invalid Rego throw errors", func(t *testing.T) {
		logger.Info("Running test: Templates with Invalid Rego throw errors")
		// Create template with invalid rego, should populate parse error in status
		instanceInvalidRego := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "invalidrego"},
			Spec: v1beta1.ConstraintTemplateSpec{
				CRD: v1beta1.CRD{
					Spec: v1beta1.CRDSpec{
						Names: v1beta1.Names{
							Kind: "InvalidRego",
						},
					},
				},
				Targets: []v1beta1.Target{
					{
						Target: target.Name,
						Rego: `
	package foo

	violation[{"msg": "hi"}] { 1 == 1 }

	anyrule[}}}//invalid//rego

	`,
					},
				},
			},
		}

		err = c.Create(ctx, instanceInvalidRego)
		if err != nil {
			t.Fatal(err)
		}

		// TODO: Test if this removal is necessary.
		// https://github.com/open-policy-agent/gatekeeper/pull/1595#discussion_r722819552
		t.Cleanup(testutils.DeleteObject(t, c, instanceInvalidRego))

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			ct := &v1beta1.ConstraintTemplate{}
			if err := c.Get(ctx, types.NamespacedName{Name: "invalidrego"}, ct); err != nil {
				return err
			}

			if ct.Name != "invalidrego" {
				return errors.New("InvalidRego not found")
			}

			status, found := getCTByPodStatus(ct)
			if !found {
				return fmt.Errorf("could not retrieve CT status for pod, byPod status: %+v", ct.Status.ByPod)
			}

			if len(status.Errors) == 0 {
				j, err := json.Marshal(status)
				if err != nil {
					t.Fatal("could not parse JSON", err)
				}
				s := string(j)
				return fmt.Errorf("InvalidRego template should contain an error: %q", s)
			}

			if status.Errors[0].Code != ErrIngestCode {
				return fmt.Errorf("InvalidRego template returning unexpected error %q, got error %+v",
					status.Errors[0].Code, status.Errors)
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Deleted constraint templates not enforced", func(t *testing.T) {
		suffix := "DeletedNotEnforced"

		logger.Info("Running test: Deleted constraint templates not enforced")
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testns",
			},
		}
		req := admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Namespace",
			},
			Operation: "Create",
			Name:      "FooNamespace",
			Object:    runtime.RawExtension{Object: ns},
		}
		resp, err := cfClient.Review(ctx, req, reviews.EnforcementPoint(util.AuditEnforcementPoint))
		if err != nil {
			t.Fatal(err)
		}

		gotResults := resp.Results()
		if len(resp.Results()) != 0 {
			t.Log(resp.TraceDump())
			t.Log(cfClient.Dump(ctx))
			t.Fatalf("did not get 0 results: %v", gotResults)
		}

		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		err = c.Delete(ctx, constraintTemplate)
		if err != nil && !apierrors.IsNotFound(err) {
			t.Fatal(err)
		}

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			resp, err := cfClient.Review(ctx, req, reviews.EnforcementPoint(util.AuditEnforcementPoint))
			if err != nil {
				return err
			}
			if len(resp.Results()) != 0 {
				dump, _ := cfClient.Dump(ctx)
				return fmt.Errorf("Results not yet zero\nDUMP:\n%s", dump)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

// Tests that expectations for constraints are canceled if the corresponding constraint is deleted.
func TestReconcile_DeleteConstraintResources(t *testing.T) {
	logger.Info("Running test: Cancel the expectations when constraint gets deleted")

	// Setup the Manager
	mgr, wm := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	// start manager that will start tracker and controller
	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	// Create the constraint template object and expect Reconcile to be called
	// when the controller starts.
	instance := &v1beta1.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: denyall},
		Spec: v1beta1.ConstraintTemplateSpec{
			CRD: v1beta1.CRD{
				Spec: v1beta1.CRDSpec{
					Names: v1beta1.Names{
						Kind: DenyAll,
					},
				},
			},
			Targets: []v1beta1.Target{
				{
					Target: target.Name,
					Rego: `
package foo

violation[{"msg": "denied!"}] {
	1 == 1
}
`,
				},
			},
		},
	}

	err := c.Create(ctx, instance)
	if err != nil {
		t.Fatal(err)
	}

	// TODO: Test if this removal is necessary.
	// https://github.com/open-policy-agent/gatekeeper/pull/1595#discussion_r722819552
	t.Cleanup(testutils.DeleteObject(t, c, instance))

	gvk := schema.GroupVersionKind{
		Group:   "constraints.gatekeeper.sh",
		Version: "v1beta1",
		Kind:    DenyAll,
	}

	// Install constraint CRD
	crd := makeCRD(gvk, denyall)
	err = applyCRD(ctx, c, gvk, crd)
	if err != nil {
		t.Fatalf("applying CRD: %v", err)
	}

	// Create the constraint for constraint template
	cstr := newDenyAllCstr("")
	err = c.Create(ctx, cstr)
	if err != nil {
		t.Fatal(err)
	}

	// creating the gatekeeper-system namespace is necessary because that's where
	// status resources live by default
	err = testutils.CreateGatekeeperNamespace(mgr.GetConfig())
	if err != nil {
		t.Fatal(err)
	}

	// Set up tracker
	tracker, err := readiness.SetupTrackerNoReadyz(mgr, false, false, false)
	if err != nil {
		t.Fatal(err)
	}

	driver, err := rego.New(rego.Tracing(true))
	if err != nil {
		t.Fatalf("unable to set up Driver: %v", err)
	}

	cfClient, err := constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver), constraintclient.EnforcementPoints(util.AuditEnforcementPoint))
	if err != nil {
		t.Fatalf("unable to set up constraint framework client: %s", err)
	}

	testutils.Setenv(t, "POD_NAME", "no-pod")

	cs := watch.NewSwitch()
	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	// events will be used to receive events from dynamic watches registered
	events := make(chan event.GenericEvent, 1024)
	rec, err := newReconciler(mgr, cfClient, wm, cs, tracker, events, nil, func(context.Context) (*corev1.Pod, error) { return pod, nil })
	if err != nil {
		t.Fatal(err)
	}

	err = add(mgr, rec)
	if err != nil {
		t.Fatal(err)
	}

	// get the object tracker for the constraint
	ot := tracker.For(gvk)
	tr, ok := ot.(testExpectations)
	if !ok {
		t.Fatalf("unexpected tracker, got %T", ot)
	}
	// ensure that expectations are set for the constraint gvk
	err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
		return true
	}, func() error {
		gotExpected := tr.IsExpecting(gvk, types.NamespacedName{Name: "denyallconstraint"})
		if !gotExpected {
			return errors.New("waiting for expectations")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Delete the constraint , the delete event will be reconciled by controller
	// to cancel the expectation set for it by tracker
	err = c.Delete(ctx, cstr)
	if err != nil {
		t.Fatal(err)
	}

	// set event channel to receive request for constraint
	events <- event.GenericEvent{
		Object: cstr,
	}

	// Check readiness tracker is satisfied post-reconcile
	err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
		return true
	}, func() error {
		satisfied := tracker.For(gvk).Satisfied()
		if !satisfied {
			return errors.New("not satisfied")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func constraintEnforced(ctx context.Context, c client.Client, suffix string) error {
	return retry.OnError(testutils.ConstantRetry, func(_ error) bool {
		return true
	}, func() error {
		cstr := newDenyAllCstr(suffix)
		err := c.Get(ctx, types.NamespacedName{Name: "denyallconstraint"}, cstr)
		if err != nil {
			return err
		}
		status, err := getCByPodStatus(cstr)
		if err != nil {
			return err
		}
		if !status.Enforced {
			obj, _ := json.MarshalIndent(cstr.Object, "", "  ")
			return fmt.Errorf("constraint not enforced: \n%s", obj)
		}
		return nil
	})
}

func isConstraintStatuErrorAsExpected(ctx context.Context, c client.Client, suffix string, wantErr bool, errMsg string) error {
	return retry.OnError(testutils.ConstantRetry, func(_ error) bool {
		return true
	}, func() error {
		cstr := newDenyAllCstr(suffix)
		err := c.Get(ctx, types.NamespacedName{Name: "denyallconstraint"}, cstr)
		if err != nil {
			return err
		}
		status, err := getCByPodStatus(cstr)
		if err != nil {
			return err
		}

		switch {
		case wantErr && len(status.Errors) == 0:
			return fmt.Errorf("expected error not found in status")
		case !wantErr && len(status.Errors) > 0:
			return fmt.Errorf("unexpected error found in status")
		case wantErr:
			for _, e := range status.Errors {
				if strings.Contains(e.Message, errMsg) {
					return nil
				}
			}
			return fmt.Errorf("expected error not found in status")
		}
		return nil
	})
}

func newDenyAllCstr(suffix string) *unstructured.Unstructured {
	cstr := &unstructured.Unstructured{}
	cstr.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "constraints.gatekeeper.sh",
		Version: "v1beta1",
		Kind:    DenyAll + suffix,
	})
	cstr.SetName("denyallconstraint")
	return cstr
}

func newDenyAllCstrWithScopedEA(suffix string, ep ...string) *unstructured.Unstructured {
	pts := make([]interface{}, 0, len(ep))
	for _, e := range ep {
		pts = append(pts, map[string]interface{}{"name": e})
	}
	cstr := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"enforcementAction": "scoped",
				"scopedEnforcementActions": []interface{}{
					map[string]interface{}{
						"enforcementPoints": pts,
						"action":            "deny",
					},
				},
			},
		},
	}
	cstr.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "constraints.gatekeeper.sh",
		Version: "v1beta1",
		Kind:    DenyAll + suffix,
	})
	cstr.SetName("denyallconstraint")
	return cstr
}

func getCTByPodStatus(templ *v1beta1.ConstraintTemplate) (v1beta1.ByPodStatus, bool) {
	statuses := templ.Status.ByPod
	for _, s := range statuses {
		if s.ID == util.GetID() {
			return s, true
		}
	}
	return v1beta1.ByPodStatus{}, false
}

func getCByPodStatus(obj *unstructured.Unstructured) (*statusv1beta1.ConstraintPodStatusStatus, error) {
	list, found, err := unstructured.NestedSlice(obj.Object, "status", "byPod")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("no byPod status is set")
	}
	var status *statusv1beta1.ConstraintPodStatusStatus
	for _, v := range list {
		j, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		s := &statusv1beta1.ConstraintPodStatusStatus{}
		if err := json.Unmarshal(j, s); err != nil {
			return nil, err
		}
		if s.ID == util.GetID() {
			status = s
			break
		}
	}
	if status == nil {
		return nil, errors.New("current pod is not listed in byPod status")
	}
	return status, nil
}

// makeCRD generates a CRD specified by GVK and plural for testing.
func makeCRD(gvk schema.GroupVersionKind, plural string) *apiextensionsv1.CustomResourceDefinition {
	trueBool := true
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s.%s", plural, gvk.Group),
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "CustomResourceDefinition",
			APIVersion: "apiextensions/v1",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: gvk.Group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   plural,
				Singular: strings.ToLower(gvk.Kind),
				Kind:     gvk.Kind,
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    gvk.Version,
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							XPreserveUnknownFields: &trueBool,
						},
					},
				},
			},
			Scope: apiextensionsv1.ClusterScoped,
		},
	}
}

// applyCRD applies a CRD and waits for it to register successfully.
func applyCRD(ctx context.Context, client client.Client, gvk schema.GroupVersionKind, crd client.Object) error {
	err := client.Create(ctx, crd)
	if err != nil {
		return err
	}

	u := &unstructured.UnstructuredList{}
	u.SetGroupVersionKind(gvk)
	return retry.OnError(testutils.ConstantRetry, func(_ error) bool {
		return true
	}, func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return client.List(ctx, u)
	})
}

// This interface is getting used by tests to check the private objects of objectTracker.
type testExpectations interface {
	IsExpecting(gvk schema.GroupVersionKind, nsName types.NamespacedName) bool
}
