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
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	templatesv1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/reviews"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/constraint"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/webhookconfig/webhookconfigcache"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel"
	celSchema "github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/transform"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/webhook"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/require"
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
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	DenyAll = "DenyAll"
	denyall = "denyall"
)

// globalTestMu serializes access to all global variables (webhook.VwhName, transform.SyncVAPScope)
// across all constraint template tests to prevent race conditions.
var globalTestMu sync.Mutex

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

func makeReconcileConstraintTemplateForVap(suffix string, generateVAP *bool, ops []admissionregistrationv1.OperationType) *v1beta1.ConstraintTemplate {
	source := &celSchema.Source{
		FailurePolicy: ptr.To[string]("Fail"),
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
					Target:     target.Name,
					Operations: ops,
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

func getMatchEntryConfig() []configv1alpha1.MatchEntry {
	return []configv1alpha1.MatchEntry{
		{
			ExcludedNamespaces: []wildcard.Wildcard{"foo"},
			Processes:          []string{"*"},
		},
	}
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

	tracker, err := readiness.SetupTracker(mgr, false, false, false)
	if err != nil {
		t.Fatal(err)
	}

	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	// constraintEvents for constraint controller, constraintTemplateEvents for webhook changes
	constraintEvents := make(chan event.GenericEvent, 1024)
	constraintTemplateEvents := make(chan event.GenericEvent, 1024)
	processExcluder := process.Get()
	processExcluder.Add(getMatchEntryConfig())

	// Create webhook config cache for VAP operation tests
	webhookCache := webhookconfigcache.NewWebhookConfigCache()

	// Set up webhook configuration for VAP tests
	const testWebhookName = "gatekeeper-validating-webhook-configuration"
	webhookName := testWebhookName
	originalVwhName := webhook.VwhName
	defer func() { webhook.VwhName = originalVwhName }()
	webhook.VwhName = &webhookName

	exactMatch := admissionregistrationv1.Exact
	webhookConfig := webhookconfigcache.WebhookMatchingConfig{
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
					admissionregistrationv1.Update,
					admissionregistrationv1.Delete,
					admissionregistrationv1.Connect,
				},
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"*"},
				},
			},
		},
		MatchPolicy: &exactMatch,
	}
	webhookCache.UpsertConfig(testWebhookName, webhookConfig)

	// Make webhook cache available to tests for configuration
	sharedWebhookCache := webhookCache

	rec, err := newReconciler(mgr, cfClient, wm, tracker, constraintEvents, constraintEvents, func(context.Context) (*corev1.Pod, error) { return pod, nil }, webhookCache, processExcluder)
	if err != nil {
		t.Fatal(err)
	}

	err = add(mgr, rec, constraintTemplateEvents)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	transform.SetVapAPIEnabled(ptr.To[bool](true))
	transform.SetGroupVersion(&admissionregistrationv1.SchemeGroupVersion)

	// Override the default VAPB generation wait time to speed up tests.
	// The production default is 30s, but tests only need to verify the
	// wait behavior works, not that it waits a specific duration.
	origWait := *constraint.DefaultWaitForVAPBGeneration
	*constraint.DefaultWaitForVAPBGeneration = 2
	t.Cleanup(func() { *constraint.DefaultWaitForVAPBGeneration = origWait })

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

		logger.Info("Running Test: vapb annotation should not be present on constraint template")
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			ct := &v1beta1.ConstraintTemplate{}
			if err := c.Get(ctx, types.NamespacedName{Name: denyall + strings.ToLower(suffix)}, ct); err != nil {
				return err
			}
			if _, ok := ct.GetAnnotations()[constraint.BlockVAPBGenerationUntilAnnotation]; ok {
				return fmt.Errorf("unexpected %s annotations on CT", constraint.BlockVAPBGenerationUntilAnnotation)
			}
			if _, ok := ct.GetAnnotations()[constraint.VAPBGenerationAnnotation]; ok {
				return fmt.Errorf("unexpected %s annotations on CT", constraint.VAPBGenerationAnnotation)
			}
			return nil
		})
	})

	t.Run("Vap should be created with default version", func(t *testing.T) {
		suffix := "VapShouldBeCreatedV1Beta1"

		logger.Info("Running test: Vap should be created with default version")
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true), nil)
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

	t.Run("Vap should not be created", func(t *testing.T) {
		suffix := "VapShouldNotBeCreated"

		logger.Info("Running test: Vap should not be created")
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](false), nil)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vap resource exists now
			vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
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

		logger.Info("Running test: Vap should not be created for rego only template")
		constraintTemplate := makeReconcileConstraintTemplate(suffix)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vap resource exists now
			vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
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

		logger.Info("Running test: EnforcementPointStatus should indicate missing CEL engine for constraint using VAP enforcementPoint with rego templates")
		cstr := newDenyAllCstrWithScopedEA(suffix, util.VAPEnforcementPoint)
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
			statusObj := &statusv1beta1.ConstraintPodStatus{}
			sName, err := statusv1beta1.KeyForConstraint(util.GetPodName(), cstr)
			if err != nil {
				return err
			}
			key := types.NamespacedName{Name: sName, Namespace: util.GetNamespace()}
			if err := c.Get(ctx, key, statusObj); err != nil {
				return err
			}

			for _, ep := range statusObj.Status.EnforcementPointsStatus {
				if ep.EnforcementPoint == util.VAPEnforcementPoint {
					if ep.Message == "" {
						return fmt.Errorf("expected message")
					}
					if ep.State != constraint.ErrGenerateVAPBState {
						return fmt.Errorf("expected error code")
					}
					return nil
				}
			}
			return fmt.Errorf("expected enforcement point status")
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Vap should not be created for only rego engine", func(t *testing.T) {
		suffix := "VapShouldNotBeCreatedForOnlyRegoEngine"

		logger.Info("Running test: Vap should not be created for only rego engine")
		constraintTemplate := makeReconcileConstraintTemplateWithRegoEngine(suffix)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vap resource exists now
			vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
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
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, nil, nil)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			// check if vap resource exists now
			vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
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
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, nil, nil)
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

		logger.Info("Running test: constraint template should have vap generated status state set to generated")
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			statusObj := &statusv1beta1.ConstraintTemplatePodStatus{}
			sName, err := statusv1beta1.KeyForConstraintTemplate(util.GetPodName(), constraintTemplate.GetName())
			if err != nil {
				return err
			}
			key := types.NamespacedName{Name: sName, Namespace: util.GetNamespace()}
			if err := c.Get(ctx, key, statusObj); err != nil {
				return err
			}

			if statusObj.Status.VAPGenerationStatus == nil || statusObj.Status.VAPGenerationStatus.State != GeneratedVAPState {
				return fmt.Errorf("Expected VAP generation status state to be %s", GeneratedVAPState)
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
		require.NoError(t, flag.CommandLine.Parse([]string{"--default-create-vap-binding-for-constraints", "false"}))
		t.Cleanup(func() {
			require.NoError(t, flag.CommandLine.Parse([]string{"--default-create-vap-binding-for-constraints", "true"}))
		})
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](false), nil)
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
			if err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("gatekeeper-%s", cstr.GetName())}, vapBinding); err != nil {
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
			vapBinding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
			if err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("gatekeeper-%s", cstr.GetName())}, vapBinding); err != nil {
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
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To(false), nil)
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

		err := isConstraintStatuErrorAsExpected(ctx, c, suffix, true, constraint.ErrVAPConditionsNotSatisfied.Error())
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Error should not be present on constraint when VAP generation if off and VAPB generation is on for templates without CEL", func(t *testing.T) {
		suffix := "ErrorShouldNotBePresentOnConstraint"
		logger.Info("Running test: Error should not be present on constraint when VAP generation is off and VAPB generation is on for templates wihout CEL")
		require.NoError(t, flag.CommandLine.Parse([]string{"--default-create-vap-for-templates", "false"}))
		t.Cleanup(func() {
			require.NoError(t, flag.CommandLine.Parse([]string{"--default-create-vap-for-templates", "true"}))
		})
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

	t.Run("  be created without generateVap intent in CT", func(t *testing.T) {
		suffix := "VapBindingShouldNotBeCreatedWithoutGenerateVapIntent"
		logger.Info("Running test: VapBinding should not be created without generateVap intent in CT")
		constraint.DefaultGenerateVAPB = ptr.To[bool](true)
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](false), nil)
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
			vapBinding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
			if err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("gatekeeper-%s", cstr.GetName())}, vapBinding); err != nil {
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

	t.Run("VapBinding should be created with VAP enforcement point after default wait", func(t *testing.T) {
		suffix := "VapBindingShouldBeCreatedWithVAPEnforcementPoint"
		logger.Info("Running test: VapBinding should be created with VAP enforcement point after default wait")
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true), nil)
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
			ct := &v1beta1.ConstraintTemplate{}
			if err := c.Get(ctx, types.NamespacedName{Name: denyall + strings.ToLower(suffix)}, ct); err != nil {
				return err
			}
			timestamp := ct.GetAnnotations()[constraint.BlockVAPBGenerationUntilAnnotation]
			if timestamp == "" {
				return fmt.Errorf("expected %s annotations on CT", constraint.BlockVAPBGenerationUntilAnnotation)
			}
			if err := c.Get(ctx, types.NamespacedName{Name: cstr.GetName()}, cstr); err != nil {
				return err
			}
			// check if vapbinding resource exists now
			vapBinding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
			if err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("gatekeeper-%s", cstr.GetName())}, vapBinding); err != nil {
				// Since tests retries 3000 times at 100 retries per second, adding sleep makes sure that this test gets covarage time > 30s to cover the default wait.
				time.Sleep(10 * time.Millisecond)
				return err
			}
			blockTime, err := time.Parse(time.RFC3339, timestamp)
			if err != nil {
				return err
			}
			vapBindingCreationTime := vapBinding.GetCreationTimestamp().Time
			if vapBindingCreationTime.Before(blockTime) {
				return fmt.Errorf("VAPBinding should be created after default wait")
			}
			if ct.GetAnnotations()[constraint.VAPBGenerationAnnotation] != constraint.VAPBGenerationUnblocked {
				return fmt.Errorf("expected %s annotations on CT to be unblocked", constraint.VAPBGenerationAnnotation)
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
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true), nil)
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
			vapBinding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
			if err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("gatekeeper-%s", cstr.GetName())}, vapBinding); err != nil {
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
		transform.SetGroupVersion(&admissionregistrationv1.SchemeGroupVersion)
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true), nil)
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

		logger.Info("Running test: VAP ConstraintTemplate should have VAPB annotation")
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			ct := &v1beta1.ConstraintTemplate{}
			if err := c.Get(ctx, types.NamespacedName{Name: denyall + strings.ToLower(suffix)}, ct); err != nil {
				return err
			}
			if ct.GetAnnotations()[constraint.BlockVAPBGenerationUntilAnnotation] == "" {
				return fmt.Errorf("expected %s annotations on CT", constraint.BlockVAPBGenerationUntilAnnotation)
			}
			if ct.GetAnnotations()[constraint.VAPBGenerationAnnotation] == "" {
				return fmt.Errorf("expected %s annotations on CT", constraint.VAPBGenerationAnnotation)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VAP should be created with operations", func(t *testing.T) {
		suffix := "VapShouldBeCreatedWithOps"

		logger.Info("Running test: VAP should be created with operations")
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		cache := webhookconfigcache.NewWebhookConfigCache()
		const testWebhookName = "gatekeeper-validating-webhook-configuration"
		webhookName := testWebhookName
		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName

		exactMatch := admissionregistrationv1.Exact
		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create,
						admissionregistrationv1.Update,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"*"},
					},
				},
			},
			MatchPolicy: &exactMatch,
		}
		cache.UpsertConfig(webhookName, webhookConfig)

		transform.SetGroupVersion(&admissionregistrationv1.SchemeGroupVersion)
		ro := []admissionregistrationv1.OperationType{
			admissionregistrationv1.Create,
			admissionregistrationv1.Update,
		}
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true), ro)
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
			vapResourceRuleOps := vap.Spec.MatchConstraints.ResourceRules[0].Operations
			if !sets.NewString(stringSliceFromOps(vapResourceRuleOps)...).Equal(sets.NewString(stringSliceFromOps(ro)...)) {
				return fmt.Errorf("expected operations %v on VAP", ro)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VAP should be created with wildcard operations", func(t *testing.T) {
		suffix := "VapShouldBeCreatedWithWildcardOps"

		logger.Info("Running test: VAP should be created with wildcard operations")
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		cache := webhookconfigcache.NewWebhookConfigCache()
		const testWebhookName = "gatekeeper-validating-webhook-configuration"
		webhookName := testWebhookName
		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName

		exactMatch := admissionregistrationv1.Exact
		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create,
						admissionregistrationv1.Update,
						admissionregistrationv1.Delete,
						admissionregistrationv1.Connect,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"*"},
					},
				},
			},
			MatchPolicy: &exactMatch,
		}
		cache.UpsertConfig(webhookName, webhookConfig)

		transform.SetGroupVersion(&admissionregistrationv1.SchemeGroupVersion)
		ro := []admissionregistrationv1.OperationType{
			admissionregistrationv1.OperationAll,
		}
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true), ro)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
			vapName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))
			if err := c.Get(ctx, types.NamespacedName{Name: vapName}, vap); err != nil {
				return err
			}
			vapResourceRuleOps := vap.Spec.MatchConstraints.ResourceRules[0].Operations
			expectedOps := []admissionregistrationv1.OperationType{
				admissionregistrationv1.Create, admissionregistrationv1.Update,
				admissionregistrationv1.Delete, admissionregistrationv1.Connect,
			}
			if !sets.NewString(stringSliceFromOps(vapResourceRuleOps)...).Equal(sets.NewString(stringSliceFromOps(expectedOps)...)) {
				return fmt.Errorf("expected wildcard operations to expand to %v, got %v", expectedOps, vapResourceRuleOps)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VAP should be created with all individual operations", func(t *testing.T) {
		suffix := "VapShouldBeCreatedWithAllOps"

		logger.Info("Running test: VAP should be created with all individual operations")
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		cache := webhookconfigcache.NewWebhookConfigCache()
		const testWebhookName = "gatekeeper-validating-webhook-configuration"
		webhookName := testWebhookName
		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName

		exactMatch := admissionregistrationv1.Exact
		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create,
						admissionregistrationv1.Update,
						admissionregistrationv1.Delete,
						admissionregistrationv1.Connect,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"*"},
					},
				},
			},
			MatchPolicy: &exactMatch,
		}
		cache.UpsertConfig(webhookName, webhookConfig)

		transform.SetGroupVersion(&admissionregistrationv1.SchemeGroupVersion)
		ro := []admissionregistrationv1.OperationType{
			admissionregistrationv1.Create,
			admissionregistrationv1.Update,
			admissionregistrationv1.Delete,
			admissionregistrationv1.Connect,
		}
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true), ro)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
			vapName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))
			if err := c.Get(ctx, types.NamespacedName{Name: vapName}, vap); err != nil {
				return err
			}
			vapResourceRuleOps := vap.Spec.MatchConstraints.ResourceRules[0].Operations
			if !sets.NewString(stringSliceFromOps(vapResourceRuleOps)...).Equal(sets.NewString(stringSliceFromOps(ro)...)) {
				return fmt.Errorf("expected operations %v on VAP", ro)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VAP should be created with DELETE operation", func(t *testing.T) {
		suffix := "VapShouldBeCreatedWithDeleteOp"

		logger.Info("Running test: VAP should be created with DELETE operation")
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		cache := webhookconfigcache.NewWebhookConfigCache()
		const testWebhookName = "gatekeeper-validating-webhook-configuration"
		webhookName := testWebhookName
		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName

		exactMatch := admissionregistrationv1.Exact
		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Delete,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"*"},
					},
				},
			},
			MatchPolicy: &exactMatch,
		}
		cache.UpsertConfig(webhookName, webhookConfig)

		transform.SetGroupVersion(&admissionregistrationv1.SchemeGroupVersion)
		ro := []admissionregistrationv1.OperationType{
			admissionregistrationv1.Delete,
		}
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true), ro)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
			vapName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))
			if err := c.Get(ctx, types.NamespacedName{Name: vapName}, vap); err != nil {
				return err
			}
			vapResourceRuleOps := vap.Spec.MatchConstraints.ResourceRules[0].Operations
			if !sets.NewString(stringSliceFromOps(vapResourceRuleOps)...).Equal(sets.NewString(stringSliceFromOps(ro)...)) {
				return fmt.Errorf("expected operations %v on VAP", ro)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VAP should be created with empty operations using webhook defaults", func(t *testing.T) {
		suffix := "VapShouldBeCreatedWithEmptyOps"

		logger.Info("Running test: VAP should be created with empty operations using webhook defaults")
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true
		// Use the shared webhook cache instead of creating a new one
		cache := sharedWebhookCache
		const testWebhookName = "gatekeeper-validating-webhook-configuration"
		webhookName := testWebhookName
		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName

		exactMatch := admissionregistrationv1.Exact
		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Delete,
						admissionregistrationv1.Update,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"*"},
					},
				},
			},
			MatchPolicy: &exactMatch,
		}
		cache.UpsertConfig(webhookName, webhookConfig)
		ro := []admissionregistrationv1.OperationType{
			admissionregistrationv1.Delete,
			admissionregistrationv1.Update,
		}

		transform.SetGroupVersion(&admissionregistrationv1.SchemeGroupVersion)
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true), nil)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
			vapName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))
			if err := c.Get(ctx, types.NamespacedName{Name: vapName}, vap); err != nil {
				return err
			}
			if vap.Spec.MatchConstraints == nil || len(vap.Spec.MatchConstraints.ResourceRules) == 0 {
				return fmt.Errorf("expected VAP to have resource rules")
			}
			vapResourceRuleOps := vap.Spec.MatchConstraints.ResourceRules[0].Operations
			if !sets.NewString(stringSliceFromOps(vapResourceRuleOps)...).Equal(sets.NewString(stringSliceFromOps(ro)...)) {
				return fmt.Errorf("expected operations %v on VAP", ro)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VapBinding should be created with v1 with some delay after constraint CRD is available", func(t *testing.T) {
		suffix := "VapBindingShouldBeCreatedV1"
		logger.Info("Running test: VapBinding should be created with v1 with some delay after constraint CRD is available")
		transform.SetGroupVersion(&admissionregistrationv1.SchemeGroupVersion)
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true), nil)
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
			ct := &v1beta1.ConstraintTemplate{}
			if err := c.Get(ctx, types.NamespacedName{Name: denyall + strings.ToLower(suffix)}, ct); err != nil {
				return err
			}
			timestamp := ct.GetAnnotations()[constraint.BlockVAPBGenerationUntilAnnotation]
			if timestamp == "" {
				return fmt.Errorf("expected %s annotations on CT", constraint.BlockVAPBGenerationUntilAnnotation)
			}
			blockTime, err := time.Parse(time.RFC3339, timestamp)
			if err != nil {
				return err
			}
			// check if vapbinding resource exists now
			vapBinding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
			if err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("gatekeeper-%s", cstr.GetName())}, vapBinding); err != nil {
				// Since tests retries 3000 times at 100 retries per second, adding sleep makes sure that this test gets covarage time > 30s to cover the default wait.
				time.Sleep(10 * time.Millisecond)
				return err
			}

			vapBindingCreationTime := vapBinding.GetCreationTimestamp().Time
			if vapBindingCreationTime.Before(blockTime) {
				return fmt.Errorf("VAPBinding should not be created before the timestamp")
			}
			if ct.GetAnnotations()[constraint.VAPBGenerationAnnotation] != constraint.VAPBGenerationUnblocked {
				return fmt.Errorf("expected %s annotations on CT to be unblocked", constraint.VAPBGenerationAnnotation)
			}
			if err := c.Delete(ctx, cstr); err != nil {
				return err
			}
			return c.Delete(ctx, vapBinding)
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VapBinding should be created with v1 without warnings in enforcementPointsStatus", func(t *testing.T) {
		suffix := "VapBindingShouldBeCreatedV1EnforcementPointsStatus"
		logger.Info("Running test: VapBinding should be created with v1 without warnings in enforcementPointsStatus")
		transform.SetGroupVersion(&admissionregistrationv1.SchemeGroupVersion)
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true), nil)
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
			statusObj := &statusv1beta1.ConstraintTemplatePodStatus{}
			sName, err := statusv1beta1.KeyForConstraintTemplate(util.GetPodName(), constraintTemplate.GetName())
			if err != nil {
				return err
			}
			key := types.NamespacedName{Name: sName, Namespace: util.GetNamespace()}
			if err := c.Get(ctx, key, statusObj); err != nil {
				return err
			}

			if statusObj.Status.VAPGenerationStatus == nil || statusObj.Status.VAPGenerationStatus.State != GeneratedVAPState {
				return fmt.Errorf("Expected VAP generation status state to be %s", GeneratedVAPState)
			}

			cStatusObj := &statusv1beta1.ConstraintPodStatus{}
			name, err := statusv1beta1.KeyForConstraint(util.GetPodName(), cstr)
			if err != nil {
				return err
			}
			key = types.NamespacedName{Name: name, Namespace: util.GetNamespace()}
			if err := c.Get(ctx, key, cStatusObj); err != nil {
				return err
			}

			for _, ep := range cStatusObj.Status.EnforcementPointsStatus {
				if ep.EnforcementPoint == util.VAPEnforcementPoint {
					if ep.Message != "" {
						return fmt.Errorf("message not expected")
					}
					if ep.State != constraint.GeneratedVAPBState {
						return fmt.Errorf("expected state to be %s", constraint.GeneratedVAPBState)
					}
					break
				}
			}
			return c.Delete(ctx, cstr)
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VAP default version should be recreated when deleted", func(t *testing.T) {
		suffix := "VapV1Beta1ShouldBeRecreated"

		logger.Info("Running test: VAP default version should be recreated when deleted")
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true), nil)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		vapName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))

		// First, wait for VAP to be created
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
			if err := c.Get(ctx, types.NamespacedName{Name: vapName}, vap); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		// Delete the VAP resource directly to simulate external deletion
		vapToDelete := &admissionregistrationv1.ValidatingAdmissionPolicy{}
		err = c.Get(ctx, types.NamespacedName{Name: vapName}, vapToDelete)
		if err != nil {
			t.Fatal(err)
		}

		err = c.Delete(ctx, vapToDelete)
		if err != nil {
			t.Fatal(err)
		}

		// Verify the VAP is recreated by the watch
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
			if err := c.Get(ctx, types.NamespacedName{Name: vapName}, vap); err != nil {
				return err
			}
			// Check that this is a new VAP instance (different UID)
			if vap.UID == vapToDelete.UID {
				return fmt.Errorf("VAP was not recreated, same UID found")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VAP v1 should be recreated when deleted", func(t *testing.T) {
		suffix := "VapV1ShouldBeRecreated"

		logger.Info("Running test: VAP v1 should be recreated when deleted")
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true), nil)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		transform.SetGroupVersion(&admissionregistrationv1.SchemeGroupVersion)
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		vapName := fmt.Sprintf("gatekeeper-%s", denyall+strings.ToLower(suffix))

		// First, wait for VAP to be created
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
			if err := c.Get(ctx, types.NamespacedName{Name: vapName}, vap); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		// Delete the VAP resource directly to simulate external deletion
		vapToDelete := &admissionregistrationv1.ValidatingAdmissionPolicy{}
		err = c.Get(ctx, types.NamespacedName{Name: vapName}, vapToDelete)
		if err != nil {
			t.Fatal(err)
		}

		err = c.Delete(ctx, vapToDelete)
		if err != nil {
			t.Fatal(err)
		}

		// Verify the VAP is recreated by the watch
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
			if err := c.Get(ctx, types.NamespacedName{Name: vapName}, vap); err != nil {
				return err
			}
			// Check that this is a new VAP instance (different UID)
			if vap.UID == vapToDelete.UID {
				return fmt.Errorf("VAP was not recreated, same UID found")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VAPB default version should be recreated when deleted", func(t *testing.T) {
		suffix := "VapbV1Beta1ShouldBeRecreated"

		logger.Info("Running test: VAPB default version should be recreated when deleted")
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true), nil)
		cstr := newDenyAllCstr(suffix)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		// Create the constraint first
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			return c.Create(ctx, cstr)
		})
		if err != nil {
			t.Fatal(err)
		}

		vapbName := fmt.Sprintf("gatekeeper-%s", cstr.GetName())

		// First, wait for VAPB to be created
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			vapb := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
			if err := c.Get(ctx, types.NamespacedName{Name: vapbName}, vapb); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		// Delete the VAPB resource directly to simulate external deletion
		vapbToDelete := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
		err = c.Get(ctx, types.NamespacedName{Name: vapbName}, vapbToDelete)
		if err != nil {
			t.Fatal(err)
		}

		err = c.Delete(ctx, vapbToDelete)
		if err != nil {
			t.Fatal(err)
		}

		// Verify the VAPB is recreated by the watch
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			vapb := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
			if err := c.Get(ctx, types.NamespacedName{Name: vapbName}, vapb); err != nil {
				return err
			}
			// Check that this is a new VAPB instance (different UID)
			if vapb.UID == vapbToDelete.UID {
				return fmt.Errorf("VAPB was not recreated, same UID found")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		// Clean up the constraint
		err = c.Delete(ctx, cstr)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("VAPB v1 should be recreated when deleted", func(t *testing.T) {
		suffix := "VapbV1ShouldBeRecreated"

		logger.Info("Running test: VAPB v1 should be recreated when deleted")
		constraintTemplate := makeReconcileConstraintTemplateForVap(suffix, ptr.To[bool](true), nil)
		cstr := newDenyAllCstr(suffix)
		t.Cleanup(testutils.DeleteObjectAndConfirm(ctx, t, c, expectedCRD(suffix)))
		transform.SetGroupVersion(&admissionregistrationv1.SchemeGroupVersion)
		testutils.CreateThenCleanup(ctx, t, c, constraintTemplate)

		// Create the constraint first
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			return c.Create(ctx, cstr)
		})
		if err != nil {
			t.Fatal(err)
		}

		vapbName := fmt.Sprintf("gatekeeper-%s", cstr.GetName())

		// First, wait for VAPB to be created
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			vapb := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
			if err := c.Get(ctx, types.NamespacedName{Name: vapbName}, vapb); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		// Delete the VAPB resource directly to simulate external deletion
		vapbToDelete := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
		err = c.Get(ctx, types.NamespacedName{Name: vapbName}, vapbToDelete)
		if err != nil {
			t.Fatal(err)
		}

		err = c.Delete(ctx, vapbToDelete)
		if err != nil {
			t.Fatal(err)
		}

		// Verify the VAPB is recreated by the watch
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			vapb := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
			if err := c.Get(ctx, types.NamespacedName{Name: vapbName}, vapb); err != nil {
				return err
			}
			// Check that this is a new VAPB instance (different UID)
			if vapb.UID == vapbToDelete.UID {
				return fmt.Errorf("VAPB was not recreated, same UID found")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		// Clean up the constraint
		err = c.Delete(ctx, cstr)
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

	t.Run("Review request initiated from an enforcement point not supported by client should result in error", func(t *testing.T) {
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
			err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("cstr-%s", strings.ToLower(suffix))}, cstr)
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
	cstr.SetName("denyallconstraint")
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

	pod := fakes.Pod(
		fakes.WithNamespace("gatekeeper-system"),
		fakes.WithName("no-pod"),
	)

	// constraintEvents for constraint controller, constraintTemplateEvents for webhook changes
	constraintEvents := make(chan event.GenericEvent, 1024)
	constraintTemplateEvents := make(chan event.GenericEvent, 1024)
	processExcluder := process.Get()
	processExcluder.Add(getMatchEntryConfig())
	rec, err := newReconciler(mgr, cfClient, wm, tracker, constraintEvents, nil, func(context.Context) (*corev1.Pod, error) { return pod, nil }, nil, processExcluder)
	if err != nil {
		t.Fatal(err)
	}

	err = add(mgr, rec, constraintTemplateEvents)
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
	constraintEvents <- event.GenericEvent{
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
		err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("cstr-%s", strings.ToLower(suffix))}, cstr)
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

func stringSliceFromOps(ops []admissionregistrationv1.OperationType) []string {
	result := make([]string, len(ops))
	for i, op := range ops {
		result[i] = string(op)
	}
	return result
}

func isConstraintStatuErrorAsExpected(ctx context.Context, c client.Client, suffix string, wantErr bool, errMsg string) error {
	return retry.OnError(testutils.ConstantRetry, func(_ error) bool {
		return true
	}, func() error {
		cstr := newDenyAllCstr(suffix)
		err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("cstr-%s", strings.ToLower(suffix))}, cstr)
		if err != nil {
			return err
		}
		status, err := getCByPodStatus(cstr)
		if err != nil {
			return err
		}

		switch {
		case wantErr && len(status.Errors) == 0:
			return fmt.Errorf("expected error not found in status, no errors present")
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
	cstr.SetName(fmt.Sprintf("cstr-%s", strings.ToLower(suffix)))
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
	cstr.SetName(fmt.Sprintf("cstr-%s", strings.ToLower(suffix)))
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

// Test_getWebhookConfigFromCache tests the getWebhookConfigFromCache function.
func Test_getWebhookConfigFromCache(t *testing.T) {
	logger := logr.Discard()

	t.Run("returns nil when cache is nil", func(t *testing.T) {
		r := &ReconcileConstraintTemplate{
			webhookCache: nil,
		}

		config := r.getWebhookConfigFromCache(logger)
		require.Nil(t, config)
	})

	t.Run("returns nil when config not found in cache", func(t *testing.T) {
		// Create a webhook cache with an event channel
		cache := webhookconfigcache.NewWebhookConfigCache()
		r := &ReconcileConstraintTemplate{
			webhookCache: cache,
		}

		config := r.getWebhookConfigFromCache(logger)
		// Since we haven't set any config in the cache, it should return nil
		require.Nil(t, config)
	})
}

// Test_transformTemplateToVAP tests the transformTemplateToVAP function.
func Test_transformTemplateToVAP(t *testing.T) {
	logger := logr.Discard()
	const testWebhookName = "gatekeeper-validating-webhook-configuration"

	// Create a minimal CEL-based ConstraintTemplate for testing
	source := &celSchema.Source{
		FailurePolicy: ptr.To[string]("Fail"),
		Variables: []celSchema.Variable{
			{
				Name:       "test_var",
				Expression: "true",
			},
		},
		Validations: []celSchema.Validation{
			{
				Expression: "1 == 1",
				Message:    "test message",
			},
		},
	}

	ct := &v1beta1.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "test-template"},
		Spec: v1beta1.ConstraintTemplateSpec{
			CRD: v1beta1.CRD{
				Spec: v1beta1.CRDSpec{
					Names: v1beta1.Names{
						Kind: "TestTemplate",
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

	scheme := runtime.NewScheme()
	_ = v1beta1.AddToScheme(scheme)

	unversionedCT := &templates.ConstraintTemplate{}
	err := scheme.Convert(ct, unversionedCT, nil)
	require.NoError(t, err)

	// Save and restore transform.SyncVAPScope
	oldSyncVAPScope := *transform.SyncVAPScope
	defer func() { *transform.SyncVAPScope = oldSyncVAPScope }()

	t.Run("SyncVAPScope disabled uses default config", func(t *testing.T) {
		*transform.SyncVAPScope = false
		r := &ReconcileConstraintTemplate{}

		vap, err := r.transformTemplateToVAP(unversionedCT, "test-vap", logger)
		require.NoError(t, err)
		require.NotNil(t, vap)
		// The VAP name is derived from the template name, not the vapName parameter
		require.Equal(t, "gatekeeper-test-template", vap.Name)
	})

	t.Run("SyncVAPScope enabled with nil caches", func(t *testing.T) {
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true
		r := &ReconcileConstraintTemplate{
			processExcluder: nil,
			webhookCache:    nil,
		}

		vap, err := r.transformTemplateToVAP(unversionedCT, "test-vap-synced", logger)
		require.NoError(t, err)
		require.NotNil(t, vap)
		// The VAP name is derived from the template name
		require.Equal(t, "gatekeeper-test-template", vap.Name)
	})

	t.Run("SyncVAPScope enabled with webhook config - adds match constraints and conditions", func(t *testing.T) {
		// Serialize access to webhook.VwhName global variable
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		cache := webhookconfigcache.NewWebhookConfigCache()
		webhookName := testWebhookName

		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName

		exactMatch := admissionregistrationv1.Exact
		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"environment": "production",
				},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "tier",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"frontend", "backend"},
					},
				},
			},
			ObjectSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "critical",
				},
			},
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create,
						admissionregistrationv1.Update,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments", "statefulsets"},
					},
				},
			},
			MatchPolicy: &exactMatch,
			MatchConditions: []admissionregistrationv1.MatchCondition{
				{
					Name:       "exclude-namespace",
					Expression: `object.metadata.namespace != "kube-system"`,
				},
				{
					Name:       "require-label",
					Expression: `has(object.metadata.labels) && has(object.metadata.labels.team)`,
				},
			},
		}

		cache.UpsertConfig(webhookName, webhookConfig)

		r := &ReconcileConstraintTemplate{
			processExcluder: nil,
			webhookCache:    cache,
		}

		vap, err := r.transformTemplateToVAP(unversionedCT, "test-vap-with-webhook-config", logger)
		require.NoError(t, err)
		require.NotNil(t, vap)

		require.NotNil(t, vap.Spec.MatchConstraints)
		require.NotNil(t, vap.Spec.MatchConstraints.NamespaceSelector)
		require.NotNil(t, vap.Spec.MatchConstraints.ObjectSelector)

		require.Equal(t, "production", vap.Spec.MatchConstraints.NamespaceSelector.MatchLabels["environment"])
		require.Len(t, vap.Spec.MatchConstraints.NamespaceSelector.MatchExpressions, 1)
		require.Equal(t, "tier", vap.Spec.MatchConstraints.NamespaceSelector.MatchExpressions[0].Key)

		require.Equal(t, "critical", vap.Spec.MatchConstraints.ObjectSelector.MatchLabels["app"])

		require.NotNil(t, vap.Spec.MatchConstraints.ResourceRules)
		require.Len(t, vap.Spec.MatchConstraints.ResourceRules, 1)
		require.Contains(t, vap.Spec.MatchConstraints.ResourceRules[0].Operations, admissionregistrationv1beta1.Create)
		require.Contains(t, vap.Spec.MatchConstraints.ResourceRules[0].Operations, admissionregistrationv1beta1.Update)
		require.Equal(t, []string{"apps"}, vap.Spec.MatchConstraints.ResourceRules[0].APIGroups)
		require.Equal(t, []string{"v1"}, vap.Spec.MatchConstraints.ResourceRules[0].APIVersions)
		require.Equal(t, []string{"deployments", "statefulsets"}, vap.Spec.MatchConstraints.ResourceRules[0].Resources)

		require.NotNil(t, vap.Spec.MatchConstraints.MatchPolicy)
		exactMatchBeta := admissionregistrationv1beta1.Exact
		require.Equal(t, &exactMatchBeta, vap.Spec.MatchConstraints.MatchPolicy)

		require.NotNil(t, vap.Spec.MatchConditions)
		webhookConditionsFound := 0
		for _, cond := range vap.Spec.MatchConditions {
			if cond.Name == "exclude-namespace" {
				require.Equal(t, `object.metadata.namespace != "kube-system"`, cond.Expression)
				webhookConditionsFound++
			}
			if cond.Name == "require-label" {
				require.Equal(t, `has(object.metadata.labels) && has(object.metadata.labels.team)`, cond.Expression)
				webhookConditionsFound++
			}
		}
		require.Equal(t, 2, webhookConditionsFound)
	})

	t.Run("SyncVAPScope enabled with excluded namespaces - adds to match conditions", func(t *testing.T) {
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		processExcluder := process.Get()
		processExcluder.Add([]configv1alpha1.MatchEntry{
			{
				ExcludedNamespaces: []wildcard.Wildcard{"kube-system", "gatekeeper-system"},
				Processes:          []string{"webhook"},
			},
		})

		cache := webhookconfigcache.NewWebhookConfigCache()

		r := &ReconcileConstraintTemplate{
			processExcluder: processExcluder,
			webhookCache:    cache,
		}

		vap, err := r.transformTemplateToVAP(unversionedCT, "test-vap-with-excluded-ns", logger)
		require.NoError(t, err)
		require.NotNil(t, vap)

		require.NotNil(t, vap.Spec.MatchConditions)
		hasExcludedNsCondition := false
		for _, cond := range vap.Spec.MatchConditions {
			if cond.Name == "gatekeeper_internal_match_global_excluded_namespaces" {
				hasExcludedNsCondition = true
				require.Contains(t, cond.Expression, "kube-system")
				require.Contains(t, cond.Expression, "gatekeeper-system")
				break
			}
		}
		require.True(t, hasExcludedNsCondition)
	})

	t.Run("SyncVAPScope enabled with webhook config and excluded namespaces - combines both", func(t *testing.T) {
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		processExcluder := process.Get()
		processExcluder.Add([]configv1alpha1.MatchEntry{
			{
				ExcludedNamespaces: []wildcard.Wildcard{"test-exclude"},
				Processes:          []string{"webhook"},
			},
		})

		cache := webhookconfigcache.NewWebhookConfigCache()
		webhookName := testWebhookName

		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName
		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"monitored": "true",
				},
			},
			MatchConditions: []admissionregistrationv1.MatchCondition{
				{
					Name:       "custom-condition",
					Expression: `object.metadata.name.startsWith("prod-")`,
				},
			},
		}
		cache.UpsertConfig(webhookName, webhookConfig)

		r := &ReconcileConstraintTemplate{
			processExcluder: processExcluder,
			webhookCache:    cache,
		}

		vap, err := r.transformTemplateToVAP(unversionedCT, "test-vap-combined", logger)
		require.NoError(t, err)
		require.NotNil(t, vap)

		require.NotNil(t, vap.Spec.MatchConditions)
		hasWebhookCondition := false
		hasExcludedNsCondition := false
		for _, cond := range vap.Spec.MatchConditions {
			if cond.Name == "custom-condition" {
				hasWebhookCondition = true
				require.Equal(t, `object.metadata.name.startsWith("prod-")`, cond.Expression)
			}
			if cond.Name == "gatekeeper_internal_match_global_excluded_namespaces" {
				hasExcludedNsCondition = true
				require.Contains(t, cond.Expression, "test-exclude")
			}
		}
		require.True(t, hasWebhookCondition)
		require.True(t, hasExcludedNsCondition)

		require.NotNil(t, vap.Spec.MatchConstraints)
		require.NotNil(t, vap.Spec.MatchConstraints.NamespaceSelector)
		require.Equal(t, "true", vap.Spec.MatchConstraints.NamespaceSelector.MatchLabels["monitored"])
	})

	t.Run("ConstraintTemplate with operations - intersection with webhook operations", func(t *testing.T) {
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		ctWithOps := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template-with-ops"},
			Spec: v1beta1.ConstraintTemplateSpec{
				CRD: v1beta1.CRD{
					Spec: v1beta1.CRDSpec{
						Names: v1beta1.Names{Kind: "TestTemplateWithOps"},
					},
				},
				Targets: []v1beta1.Target{
					{
						Target: target.Name,
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
							admissionregistrationv1.Update,
						},
						Code: []v1beta1.Code{
							{
								Engine: "K8sNativeValidation",
								Source: &templates.Anything{Value: source.MustToUnstructured()},
							},
						},
					},
				},
			},
		}

		scheme := runtime.NewScheme()
		_ = v1beta1.AddToScheme(scheme)
		unversionedCTWithOps := &templates.ConstraintTemplate{}
		err := scheme.Convert(ctWithOps, unversionedCTWithOps, nil)
		require.NoError(t, err)

		cache := webhookconfigcache.NewWebhookConfigCache()
		webhookName := testWebhookName
		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName

		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create,
						admissionregistrationv1.Update,
						admissionregistrationv1.Delete,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
		}
		cache.UpsertConfig(webhookName, webhookConfig)

		r := &ReconcileConstraintTemplate{webhookCache: cache}
		vap, err := r.transformTemplateToVAP(unversionedCTWithOps, "test-vap-ops-intersection", logger)
		require.NoError(t, err)
		require.NotNil(t, vap)

		operations := vap.Spec.MatchConstraints.ResourceRules[0].Operations
		require.Contains(t, operations, admissionregistrationv1beta1.Create)
		require.Contains(t, operations, admissionregistrationv1beta1.Update)
		require.NotContains(t, operations, admissionregistrationv1beta1.Delete)
		require.Len(t, operations, 2)
	})

	t.Run("ConstraintTemplate operations mismatch - logs warning but continues", func(t *testing.T) {
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		ctWithMismatchOps := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template-mismatch-ops"},
			Spec: v1beta1.ConstraintTemplateSpec{
				CRD: v1beta1.CRD{
					Spec: v1beta1.CRDSpec{
						Names: v1beta1.Names{Kind: "TestTemplateMismatchOps"},
					},
				},
				Targets: []v1beta1.Target{
					{
						Target: target.Name,
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
							admissionregistrationv1.Delete,
						},
						Code: []v1beta1.Code{
							{
								Engine: "K8sNativeValidation",
								Source: &templates.Anything{Value: source.MustToUnstructured()},
							},
						},
					},
				},
			},
		}

		scheme := runtime.NewScheme()
		_ = v1beta1.AddToScheme(scheme)
		unversionedCTMismatch := &templates.ConstraintTemplate{}
		err := scheme.Convert(ctWithMismatchOps, unversionedCTMismatch, nil)
		require.NoError(t, err)

		cache := webhookconfigcache.NewWebhookConfigCache()
		webhookName := testWebhookName
		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName

		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create,
						admissionregistrationv1.Update,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
		}
		cache.UpsertConfig(webhookName, webhookConfig)

		r := &ReconcileConstraintTemplate{webhookCache: cache}
		vap, err := r.transformTemplateToVAP(unversionedCTMismatch, "test-vap-ops-mismatch", logger)
		require.Error(t, err)
		require.Contains(t, err.Error(), "operations mismatch")
		require.NotNil(t, vap)

		operations := vap.Spec.MatchConstraints.ResourceRules[0].Operations
		require.Contains(t, operations, admissionregistrationv1beta1.Create)
		require.NotContains(t, operations, admissionregistrationv1beta1.Delete)
		require.Len(t, operations, 1)
	})

	t.Run("ConstraintTemplate no matching operations - returns error", func(t *testing.T) {
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		ctNoMatch := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template-no-match"},
			Spec: v1beta1.ConstraintTemplateSpec{
				CRD: v1beta1.CRD{
					Spec: v1beta1.CRDSpec{
						Names: v1beta1.Names{Kind: "TestTemplateNoMatch"},
					},
				},
				Targets: []v1beta1.Target{
					{
						Target: target.Name,
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Delete,
						},
						Code: []v1beta1.Code{
							{
								Engine: "K8sNativeValidation",
								Source: &templates.Anything{Value: source.MustToUnstructured()},
							},
						},
					},
				},
			},
		}

		scheme := runtime.NewScheme()
		_ = v1beta1.AddToScheme(scheme)
		unversionedCTNoMatch := &templates.ConstraintTemplate{}
		err := scheme.Convert(ctNoMatch, unversionedCTNoMatch, nil)
		require.NoError(t, err)

		cache := webhookconfigcache.NewWebhookConfigCache()
		webhookName := testWebhookName
		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName

		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create,
						admissionregistrationv1.Update,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
		}
		cache.UpsertConfig(webhookName, webhookConfig)

		r := &ReconcileConstraintTemplate{webhookCache: cache}
		_, err = r.transformTemplateToVAP(unversionedCTNoMatch, "test-vap-no-match", logger)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no matching operations")
	})

	t.Run("ConstraintTemplate with nil operations - uses all webhook operations", func(t *testing.T) {
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		ctNilOps := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template-nil-ops"},
			Spec: v1beta1.ConstraintTemplateSpec{
				CRD: v1beta1.CRD{
					Spec: v1beta1.CRDSpec{
						Names: v1beta1.Names{Kind: "TestTemplateNilOps"},
					},
				},
				Targets: []v1beta1.Target{
					{
						Target: target.Name,
						Code: []v1beta1.Code{
							{
								Engine: "K8sNativeValidation",
								Source: &templates.Anything{Value: source.MustToUnstructured()},
							},
						},
					},
				},
			},
		}

		scheme := runtime.NewScheme()
		_ = v1beta1.AddToScheme(scheme)
		unversionedCTNilOps := &templates.ConstraintTemplate{}
		err := scheme.Convert(ctNilOps, unversionedCTNilOps, nil)
		require.NoError(t, err)

		cache := webhookconfigcache.NewWebhookConfigCache()
		webhookName := testWebhookName
		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName

		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create,
						admissionregistrationv1.Update,
						admissionregistrationv1.Delete,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
		}
		cache.UpsertConfig(webhookName, webhookConfig)

		r := &ReconcileConstraintTemplate{webhookCache: cache}
		vap, err := r.transformTemplateToVAP(unversionedCTNilOps, "test-vap-nil-ops", logger)
		require.NoError(t, err)
		require.NotNil(t, vap)

		operations := vap.Spec.MatchConstraints.ResourceRules[0].Operations
		require.Contains(t, operations, admissionregistrationv1beta1.Create)
		require.Contains(t, operations, admissionregistrationv1beta1.Update)
		require.Contains(t, operations, admissionregistrationv1beta1.Delete)
		require.Len(t, operations, 3)
	})

	t.Run("ConstraintTemplate with wildcard operations - matches all webhook operations", func(t *testing.T) {
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		ctWildcard := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template-wildcard"},
			Spec: v1beta1.ConstraintTemplateSpec{
				CRD: v1beta1.CRD{
					Spec: v1beta1.CRDSpec{
						Names: v1beta1.Names{Kind: "TestTemplateWildcard"},
					},
				},
				Targets: []v1beta1.Target{
					{
						Target: target.Name,
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.OperationAll,
						},
						Code: []v1beta1.Code{
							{
								Engine: "K8sNativeValidation",
								Source: &templates.Anything{Value: source.MustToUnstructured()},
							},
						},
					},
				},
			},
		}

		scheme := runtime.NewScheme()
		_ = v1beta1.AddToScheme(scheme)
		unversionedCTWildcard := &templates.ConstraintTemplate{}
		err := scheme.Convert(ctWildcard, unversionedCTWildcard, nil)
		require.NoError(t, err)

		cache := webhookconfigcache.NewWebhookConfigCache()
		webhookName := testWebhookName
		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName

		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create,
						admissionregistrationv1.Update,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
		}
		cache.UpsertConfig(webhookName, webhookConfig)

		r := &ReconcileConstraintTemplate{webhookCache: cache}
		vap, err := r.transformTemplateToVAP(unversionedCTWildcard, "test-vap-wildcard", logger)
		require.Error(t, err)
		require.Contains(t, err.Error(), "operations mismatch")
		require.NotNil(t, vap)

		operations := vap.Spec.MatchConstraints.ResourceRules[0].Operations
		require.Contains(t, operations, admissionregistrationv1beta1.Create)
		require.Contains(t, operations, admissionregistrationv1beta1.Update)
		require.Len(t, operations, 2)
	})

	t.Run("Webhook with wildcard operations - intersects with specific CT operations", func(t *testing.T) {
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		ctSpecific := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template-specific"},
			Spec: v1beta1.ConstraintTemplateSpec{
				CRD: v1beta1.CRD{
					Spec: v1beta1.CRDSpec{
						Names: v1beta1.Names{Kind: "TestTemplateSpecific"},
					},
				},
				Targets: []v1beta1.Target{
					{
						Target: target.Name,
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
							admissionregistrationv1.Delete,
						},
						Code: []v1beta1.Code{
							{
								Engine: "K8sNativeValidation",
								Source: &templates.Anything{Value: source.MustToUnstructured()},
							},
						},
					},
				},
			},
		}

		scheme := runtime.NewScheme()
		_ = v1beta1.AddToScheme(scheme)
		unversionedCTSpecific := &templates.ConstraintTemplate{}
		err := scheme.Convert(ctSpecific, unversionedCTSpecific, nil)
		require.NoError(t, err)

		cache := webhookconfigcache.NewWebhookConfigCache()
		webhookName := testWebhookName
		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName

		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.OperationAll,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
		}
		cache.UpsertConfig(webhookName, webhookConfig)

		r := &ReconcileConstraintTemplate{webhookCache: cache}
		vap, err := r.transformTemplateToVAP(unversionedCTSpecific, "test-vap-webhook-wildcard", logger)
		require.NoError(t, err)
		require.NotNil(t, vap)

		operations := vap.Spec.MatchConstraints.ResourceRules[0].Operations
		require.Contains(t, operations, admissionregistrationv1beta1.Create)
		require.Contains(t, operations, admissionregistrationv1beta1.Delete)
		require.Len(t, operations, 2)
	})

	t.Run("Both webhook and CT with wildcard operations", func(t *testing.T) {
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		ctWildcard := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template-both-wildcard"},
			Spec: v1beta1.ConstraintTemplateSpec{
				CRD: v1beta1.CRD{
					Spec: v1beta1.CRDSpec{
						Names: v1beta1.Names{Kind: "TestTemplateBothWildcard"},
					},
				},
				Targets: []v1beta1.Target{
					{
						Target: target.Name,
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.OperationAll,
						},
						Code: []v1beta1.Code{
							{
								Engine: "K8sNativeValidation",
								Source: &templates.Anything{Value: source.MustToUnstructured()},
							},
						},
					},
				},
			},
		}

		scheme := runtime.NewScheme()
		_ = v1beta1.AddToScheme(scheme)
		unversionedCTBothWildcard := &templates.ConstraintTemplate{}
		err := scheme.Convert(ctWildcard, unversionedCTBothWildcard, nil)
		require.NoError(t, err)

		cache := webhookconfigcache.NewWebhookConfigCache()
		webhookName := testWebhookName
		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName

		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.OperationAll,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
		}
		cache.UpsertConfig(webhookName, webhookConfig)

		r := &ReconcileConstraintTemplate{webhookCache: cache}
		vap, err := r.transformTemplateToVAP(unversionedCTBothWildcard, "test-vap-both-wildcard", logger)
		require.NoError(t, err)
		require.NotNil(t, vap)

		operations := vap.Spec.MatchConstraints.ResourceRules[0].Operations
		require.Contains(t, operations, admissionregistrationv1beta1.Create)
		require.Contains(t, operations, admissionregistrationv1beta1.Update)
		require.Contains(t, operations, admissionregistrationv1beta1.Delete)
		require.Contains(t, operations, admissionregistrationv1beta1.Connect)
		require.Len(t, operations, 4)
	})

	t.Run("Webhook supports all operations and CT supports CREATE", func(t *testing.T) {
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		ctCreate := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template-create-only"},
			Spec: v1beta1.ConstraintTemplateSpec{
				CRD: v1beta1.CRD{
					Spec: v1beta1.CRDSpec{
						Names: v1beta1.Names{Kind: "TestTemplateCreateOnly"},
					},
				},
				Targets: []v1beta1.Target{
					{
						Target: target.Name,
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
						},
						Code: []v1beta1.Code{
							{
								Engine: "K8sNativeValidation",
								Source: &templates.Anything{Value: source.MustToUnstructured()},
							},
						},
					},
				},
			},
		}

		scheme := runtime.NewScheme()
		_ = v1beta1.AddToScheme(scheme)
		unversionedCTCreate := &templates.ConstraintTemplate{}
		err := scheme.Convert(ctCreate, unversionedCTCreate, nil)
		require.NoError(t, err)

		cache := webhookconfigcache.NewWebhookConfigCache()
		webhookName := testWebhookName
		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName

		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.OperationAll,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
		}
		cache.UpsertConfig(webhookName, webhookConfig)

		r := &ReconcileConstraintTemplate{webhookCache: cache}
		vap, err := r.transformTemplateToVAP(unversionedCTCreate, "test-vap-webhook-all-ct-create", logger)
		require.NoError(t, err)
		require.NotNil(t, vap)

		operations := vap.Spec.MatchConstraints.ResourceRules[0].Operations
		require.Contains(t, operations, admissionregistrationv1beta1.Create)
		require.Len(t, operations, 1)
	})

	t.Run("CT supports all operations and webhook supports CREATE", func(t *testing.T) {
		globalTestMu.Lock()
		defer globalTestMu.Unlock()

		originalSyncVAPScope := *transform.SyncVAPScope
		defer func() { *transform.SyncVAPScope = originalSyncVAPScope }()
		*transform.SyncVAPScope = true

		ctAll := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template-all-ops"},
			Spec: v1beta1.ConstraintTemplateSpec{
				CRD: v1beta1.CRD{
					Spec: v1beta1.CRDSpec{
						Names: v1beta1.Names{Kind: "TestTemplateAllOps"},
					},
				},
				Targets: []v1beta1.Target{
					{
						Target: target.Name,
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.OperationAll,
						},
						Code: []v1beta1.Code{
							{
								Engine: "K8sNativeValidation",
								Source: &templates.Anything{Value: source.MustToUnstructured()},
							},
						},
					},
				},
			},
		}

		scheme := runtime.NewScheme()
		_ = v1beta1.AddToScheme(scheme)
		unversionedCTAll := &templates.ConstraintTemplate{}
		err := scheme.Convert(ctAll, unversionedCTAll, nil)
		require.NoError(t, err)

		cache := webhookconfigcache.NewWebhookConfigCache()
		webhookName := testWebhookName
		originalVwhName := webhook.VwhName
		defer func() { webhook.VwhName = originalVwhName }()
		webhook.VwhName = &webhookName

		webhookConfig := webhookconfigcache.WebhookMatchingConfig{
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"apps"},
						APIVersions: []string{"v1"},
						Resources:   []string{"deployments"},
					},
				},
			},
		}
		cache.UpsertConfig(webhookName, webhookConfig)

		r := &ReconcileConstraintTemplate{webhookCache: cache}
		vap, err := r.transformTemplateToVAP(unversionedCTAll, "test-vap-ct-all-webhook-create", logger)
		require.Error(t, err)
		require.Contains(t, err.Error(), "operations mismatch")
		require.NotNil(t, vap)

		operations := vap.Spec.MatchConstraints.ResourceRules[0].Operations
		require.Contains(t, operations, admissionregistrationv1beta1.Create)
		require.Len(t, operations, 1)
	})
}
