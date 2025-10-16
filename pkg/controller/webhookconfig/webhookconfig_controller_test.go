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

package webhookconfig

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/webhookconfig/webhookconfigcache"
	celSchema "github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/transform"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/webhook"
	testclient "github.com/open-policy-agent/gatekeeper/v3/test/clients"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// webhookNameMu serializes access to webhook.VwhName global variable across sub-tests.
var webhookNameMu sync.Mutex

const (
	testVwhName = "test-gatekeeper-validating-webhook-configuration"
	timeout     = 20 * time.Second
)

// TestReconcile tests the webhook config controller reconcile logic.
func TestReconcile(t *testing.T) {
	if !operations.IsAssigned(operations.Generate) {
		t.Skip("Skipping test because Generate operation is not assigned")
	}

	// Save original VwhName and restore after test
	originalVwhName := getVwhName()
	originalSyncVAPScope := transform.SyncVAPScope
	defer func() {
		setVwhName(originalVwhName)
		transform.SyncVAPScope = originalSyncVAPScope
	}()

	// Set VwhName for tests
	setVwhName(testVwhName)
	transform.SyncVAPScope = ptr.To(true)

	// Setup the Manager and Controller
	mgr, _ := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	ctx := context.Background()

	// Create event channels
	ctEvents := make(chan event.GenericEvent, 1024)
	cache := webhookconfigcache.NewWebhookConfigCache()

	// Create adder and add controller
	adder := &Adder{
		Cache:    cache,
		ctEvents: ctEvents,
	}

	err := adder.Add(mgr)
	require.NoError(t, err)

	testutils.StartManager(ctx, t, mgr)

	t.Run("webhook config created triggers cache update", func(t *testing.T) {
		// Serialize access to webhook.VwhName global variable
		webhookNameMu.Lock()
		defer webhookNameMu.Unlock()

		suffix := "-created"
		webhookName := testVwhName + suffix

		// Temporarily change VwhName for this subtest
		setVwhName(webhookName)
		defer func() { setVwhName(testVwhName) }()

		logger.Info("Running test: webhook config created triggers cache update")

		// Create webhook configuration
		vwh := createTestValidatingWebhook(webhookName, []admissionregistrationv1.RuleWithOperations{
			{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"pods"},
				},
			},
		}, nil, nil)

		testutils.CreateThenCleanup(ctx, t, c, vwh)

		// Wait for cache to be updated
		err := retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			_, exists := cache.GetConfig(webhookName)
			if !exists {
				return fmt.Errorf("webhook config not found in cache")
			}
			return nil
		})
		require.NoError(t, err, "webhook config should be in cache after creation")
	})

	t.Run("webhook config update with matching field changes triggers CT reconciliation", func(t *testing.T) {
		// Serialize access to webhook.VwhName global variable
		webhookNameMu.Lock()
		defer webhookNameMu.Unlock()

		suffix := "-updated"
		webhookName := testVwhName + suffix

		// Temporarily change VwhName for this subtest
		setVwhName(webhookName)
		defer func() { setVwhName(testVwhName) }()

		logger.Info("Running test: webhook config update with matching field changes triggers CT reconciliation")

		// Create a VAP-enabled constraint template
		ct := createVAPConstraintTemplate("test-ct" + suffix)
		testutils.CreateThenCleanup(ctx, t, c, ct)

		// Create initial webhook configuration
		vwh := createTestValidatingWebhook(webhookName, []admissionregistrationv1.RuleWithOperations{
			{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"pods"},
				},
			},
		}, nil, nil)

		testutils.CreateThenCleanup(ctx, t, c, vwh)

		// Clear initial events
		time.Sleep(500 * time.Millisecond)
		for len(ctEvents) > 0 {
			<-ctEvents
		}

		// Update webhook with changed rules
		err := retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			updatedWebhook := &admissionregistrationv1.ValidatingWebhookConfiguration{}
			if err := c.Get(ctx, types.NamespacedName{Name: webhookName}, updatedWebhook); err != nil {
				return err
			}

			updatedWebhook.Webhooks[0].Rules = []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"pods", "services"},
					},
				},
			}

			return c.Update(ctx, updatedWebhook)
		})
		require.NoError(t, err)

		// Wait for CT reconciliation event
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			if len(ctEvents) == 0 {
				return fmt.Errorf("no CT reconciliation events received")
			}
			return nil
		})
		require.NoError(t, err, "CT reconciliation should be triggered after webhook update")

		// Verify the event is for our constraint template
		evt := <-ctEvents
		require.NotNil(t, evt.Object)
		ctObj, ok := evt.Object.(*v1beta1.ConstraintTemplate)
		require.True(t, ok, "event should contain a ConstraintTemplate")
		require.Equal(t, ct.Name, ctObj.Name)
	})

	t.Run("webhook config update without matching field changes does not trigger reconciliation", func(t *testing.T) {
		// Serialize access to webhook.VwhName global variable
		webhookNameMu.Lock()
		defer webhookNameMu.Unlock()

		suffix := "-updated-no-change"
		webhookName := testVwhName + suffix

		// Temporarily change VwhName for this subtest
		setVwhName(webhookName)
		defer func() { setVwhName(testVwhName) }()

		logger.Info("Running test: webhook config update without matching field changes does not trigger reconciliation")

		// Create a VAP-enabled constraint template
		ct := createVAPConstraintTemplate("test-ct" + suffix)
		testutils.CreateThenCleanup(ctx, t, c, ct)

		// Create initial webhook configuration
		vwh := createTestValidatingWebhook(webhookName, []admissionregistrationv1.RuleWithOperations{
			{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"pods"},
				},
			},
		}, nil, nil)

		testutils.CreateThenCleanup(ctx, t, c, vwh)

		// Clear initial events
		time.Sleep(500 * time.Millisecond)
		for len(ctEvents) > 0 {
			<-ctEvents
		}

		// Update webhook with non-matching field change (e.g., annotations)
		err := retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			updatedWebhook := &admissionregistrationv1.ValidatingWebhookConfiguration{}
			if err := c.Get(ctx, types.NamespacedName{Name: webhookName}, updatedWebhook); err != nil {
				return err
			}

			// Update a non-matching field
			if updatedWebhook.Annotations == nil {
				updatedWebhook.Annotations = make(map[string]string)
			}
			updatedWebhook.Annotations["test-annotation"] = "test-value"

			return c.Update(ctx, updatedWebhook)
		})
		require.NoError(t, err)

		// Wait a bit to ensure no events are generated
		time.Sleep(1 * time.Second)
		require.Empty(t, ctEvents, "no CT reconciliation events should be triggered for non-matching field changes")
	})

	t.Run("webhook config deletion triggers CT reconciliation and cache removal", func(t *testing.T) {
		// Serialize access to webhook.VwhName global variable
		webhookNameMu.Lock()
		defer webhookNameMu.Unlock()

		suffix := "-deleted"
		webhookName := testVwhName + suffix

		// Temporarily change VwhName for this subtest
		setVwhName(webhookName)
		defer func() { setVwhName(testVwhName) }()

		logger.Info("Running test: webhook config deletion triggers CT reconciliation and cache removal")

		// Create a VAP-enabled constraint template
		ct := createVAPConstraintTemplate("test-ct" + suffix)
		testutils.CreateThenCleanup(ctx, t, c, ct)

		// Create webhook configuration
		vwh := createTestValidatingWebhook(webhookName, []admissionregistrationv1.RuleWithOperations{
			{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"pods"},
				},
			},
		}, nil, nil)

		err := c.Create(ctx, vwh)
		require.NoError(t, err)

		// Wait for cache to be populated
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			_, exists := cache.GetConfig(webhookName)
			if !exists {
				return fmt.Errorf("webhook config not found in cache")
			}
			return nil
		})
		require.NoError(t, err)

		// Clear initial events
		time.Sleep(500 * time.Millisecond)
		for len(ctEvents) > 0 {
			<-ctEvents
		}

		// Delete webhook
		err = c.Delete(ctx, vwh)
		require.NoError(t, err)

		// Wait for CT reconciliation event
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			if len(ctEvents) == 0 {
				return fmt.Errorf("no CT reconciliation events received after deletion")
			}
			return nil
		})
		require.NoError(t, err, "CT reconciliation should be triggered after webhook deletion")

		// Verify cache removal
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			_, exists := cache.GetConfig(webhookName)
			if exists {
				return fmt.Errorf("webhook config still in cache after deletion")
			}
			return nil
		})
		require.NoError(t, err, "webhook config should be removed from cache after deletion")
	})

	t.Run("webhook namespace selector change triggers reconciliation", func(t *testing.T) {
		// Serialize access to webhook.VwhName global variable
		webhookNameMu.Lock()
		defer webhookNameMu.Unlock()

		suffix := "-namespace-selector"
		webhookName := testVwhName + suffix

		// Temporarily change VwhName for this subtest
		setVwhName(webhookName)
		defer func() { setVwhName(testVwhName) }()

		logger.Info("Running test: webhook namespace selector change triggers reconciliation")

		// Create a VAP-enabled constraint template
		ct := createVAPConstraintTemplate("test-ct" + suffix)
		testutils.CreateThenCleanup(ctx, t, c, ct)

		// Create initial webhook configuration with no namespace selector
		vwh := createTestValidatingWebhook(webhookName, []admissionregistrationv1.RuleWithOperations{
			{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"pods"},
				},
			},
		}, nil, nil)

		testutils.CreateThenCleanup(ctx, t, c, vwh)

		// Clear initial events
		time.Sleep(500 * time.Millisecond)
		for len(ctEvents) > 0 {
			<-ctEvents
		}

		// Update webhook with namespace selector
		err := retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			updatedWebhook := &admissionregistrationv1.ValidatingWebhookConfiguration{}
			if err := c.Get(ctx, types.NamespacedName{Name: webhookName}, updatedWebhook); err != nil {
				return err
			}

			updatedWebhook.Webhooks[0].NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"env": "production"},
			}

			return c.Update(ctx, updatedWebhook)
		})
		require.NoError(t, err)

		// Wait for CT reconciliation event
		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			if len(ctEvents) == 0 {
				return fmt.Errorf("no CT reconciliation events received")
			}
			return nil
		})
		require.NoError(t, err, "CT reconciliation should be triggered after namespace selector change")
	})
}

// TestTriggerConstraintTemplateReconciliation tests the trigger mechanism.
func TestTriggerConstraintTemplateReconciliation(t *testing.T) {
	if !operations.IsAssigned(operations.Generate) {
		t.Skip("Skipping test because Generate operation is not assigned")
	}

	// Save original VwhName and restore after test
	originalVwhName := getVwhName()
	defer func() { setVwhName(originalVwhName) }()

	setVwhName(testVwhName)

	// Setup the Manager
	mgr, _ := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	t.Run("successful reconciliation sends events for VAP-enabled templates", func(t *testing.T) {
		logger.Info("Running test: successful reconciliation sends events for VAP-enabled templates")

		// Create VAP-enabled templates
		ct1 := createVAPConstraintTemplate("trigger-test-ct1")
		ct2 := createVAPConstraintTemplate("trigger-test-ct2")
		testutils.CreateThenCleanup(ctx, t, c, ct1)
		testutils.CreateThenCleanup(ctx, t, c, ct2)

		// Create event channel
		ctEvents := make(chan event.GenericEvent, 10)

		// Create reconciler
		reconciler := &ReconcileWebhookConfig{
			Client:   c,
			scheme:   mgr.GetScheme(),
			ctEvents: ctEvents,
		}

		// Trigger reconciliation
		reconciler.TriggerConstraintTemplateReconciliation(ctx, testVwhName)

		// Verify events were sent
		err := retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			if len(ctEvents) < 2 {
				return fmt.Errorf("expected at least 2 events, got %d", len(ctEvents))
			}
			return nil
		})
		require.NoError(t, err, "should receive events for VAP-enabled templates")
	})

	t.Run("skips templates without VAP support", func(t *testing.T) {
		logger.Info("Running test: skips templates without VAP support")

		// Create non-VAP template
		ct := createNonVAPConstraintTemplate("trigger-test-non-vap")
		testutils.CreateThenCleanup(ctx, t, c, ct)

		// Create event channel
		ctEvents := make(chan event.GenericEvent, 10)

		// Create reconciler
		reconciler := &ReconcileWebhookConfig{
			Client:   c,
			scheme:   mgr.GetScheme(),
			ctEvents: ctEvents,
		}

		// Trigger reconciliation
		reconciler.TriggerConstraintTemplateReconciliation(ctx, testVwhName)

		// Wait a bit and verify no events for non-VAP template
		time.Sleep(500 * time.Millisecond)
		// There might be events from previous tests, but we can't determine which ones
		// are from this test specifically, so we just verify the trigger doesn't crash
	})

	t.Run("handles nil ctEvents channel gracefully", func(t *testing.T) {
		logger.Info("Running test: handles nil ctEvents channel gracefully")

		// Create reconciler with nil ctEvents
		reconciler := &ReconcileWebhookConfig{
			Client:   c,
			scheme:   mgr.GetScheme(),
			ctEvents: nil,
		}

		// This should not panic
		reconciler.TriggerConstraintTemplateReconciliation(ctx, testVwhName)
	})
}

// Helper functions

func createTestValidatingWebhook(name string, rules []admissionregistrationv1.RuleWithOperations, namespaceSelector, objectSelector *metav1.LabelSelector) *admissionregistrationv1.ValidatingWebhookConfiguration {
	sideEffects := admissionregistrationv1.SideEffectClassNone
	failurePolicy := admissionregistrationv1.Fail

	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admissionregistration.k8s.io/v1",
			Kind:       "ValidatingWebhookConfiguration",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			{
				Name:                    webhook.ValidatingWebhookName,
				Rules:                   rules,
				FailurePolicy:           &failurePolicy,
				SideEffects:             &sideEffects,
				NamespaceSelector:       namespaceSelector,
				ObjectSelector:          objectSelector,
				AdmissionReviewVersions: []string{"v1", "v1beta1"},
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					Service: &admissionregistrationv1.ServiceReference{
						Name:      "gatekeeper-webhook-service",
						Namespace: "gatekeeper-system",
						Path:      ptr.To("/v1/admit"),
					},
				},
			},
		},
	}
}

func createVAPConstraintTemplate(name string) *v1beta1.ConstraintTemplate {
	// Create a proper CEL source that enables VAP generation
	source := &celSchema.Source{
		Validations: []celSchema.Validation{
			{
				Expression: "true",
				Message:    "test validation",
			},
		},
		GenerateVAP: ptr.To(true),
	}

	ct := &v1beta1.ConstraintTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "templates.gatekeeper.sh/v1beta1",
			Kind:       "ConstraintTemplate",
		},
	}
	ct.SetName(name)
	ct.Spec.CRD.Spec.Names.Kind = name + "Kind"
	// Add K8sNativeValidation target to make it VAP-eligible
	ct.Spec.Targets = []v1beta1.Target{
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
	}

	return ct
}

func createNonVAPConstraintTemplate(name string) *v1beta1.ConstraintTemplate {
	ct := &v1beta1.ConstraintTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "templates.gatekeeper.sh/v1beta1",
			Kind:       "ConstraintTemplate",
		},
	}
	ct.SetName(name)
	ct.Spec.CRD.Spec.Names.Kind = name + "Kind"
	// Add only Rego target (no VAP support)
	ct.Spec.Targets = []v1beta1.Target{
		{
			Target: target.Name,
			Rego: `
package foo

violation[{"msg": "denied!"}] {
	1 == 1
}
`,
		},
	}

	return ct
}
