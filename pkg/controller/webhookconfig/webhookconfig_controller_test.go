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

var webhookNameMu sync.Mutex

const (
	testVwhName = "test-gatekeeper-validating-webhook-configuration"
	timeout     = 20 * time.Second
)

func TestReconcile(t *testing.T) {
	if !operations.IsAssigned(operations.Generate) {
		t.Skip("Skipping test because Generate operation is not assigned")
	}

	originalVwhName := getVwhName()
	originalSyncVAPScope := transform.SyncVAPScope
	defer func() {
		setVwhName(originalVwhName)
		transform.SyncVAPScope = originalSyncVAPScope
	}()

	setVwhName(testVwhName)
	transform.SyncVAPScope = ptr.To(true)

	mgr, _ := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	ctx := context.Background()

	ctEvents := make(chan event.GenericEvent, 1024)
	cache := webhookconfigcache.NewWebhookConfigCache()

	adder := &Adder{
		Cache:    cache,
		ctEvents: ctEvents,
	}

	err := adder.Add(mgr)
	require.NoError(t, err)

	testutils.StartManager(ctx, t, mgr)

	t.Run("webhook config created triggers cache update", func(t *testing.T) {
		webhookNameMu.Lock()
		defer webhookNameMu.Unlock()

		suffix := "-created"
		webhookName := testVwhName + suffix

		setVwhName(webhookName)
		defer func() { setVwhName(testVwhName) }()

		logger.Info("Running test: webhook config created triggers cache update")

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
		webhookNameMu.Lock()
		defer webhookNameMu.Unlock()

		suffix := "-updated"
		webhookName := testVwhName + suffix

		setVwhName(webhookName)
		defer func() { setVwhName(testVwhName) }()

		logger.Info("Running test: webhook config update with matching field changes triggers CT reconciliation")

		ct := createVAPConstraintTemplate("test-ct" + suffix)
		testutils.CreateThenCleanup(ctx, t, c, ct)

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

		time.Sleep(500 * time.Millisecond)
		for len(ctEvents) > 0 {
			<-ctEvents
		}

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

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			if len(ctEvents) == 0 {
				return fmt.Errorf("no CT reconciliation events received")
			}
			return nil
		})
		require.NoError(t, err, "CT reconciliation should be triggered after webhook update")

		evt := <-ctEvents
		require.NotNil(t, evt.Object)
		ctObj, ok := evt.Object.(*v1beta1.ConstraintTemplate)
		require.True(t, ok, "event should contain a ConstraintTemplate")
		require.Equal(t, ct.Name, ctObj.Name)
	})

	t.Run("webhook config update without matching field changes does not trigger reconciliation", func(t *testing.T) {
		webhookNameMu.Lock()
		defer webhookNameMu.Unlock()

		suffix := "-updated-no-change"
		webhookName := testVwhName + suffix

		setVwhName(webhookName)
		defer func() { setVwhName(testVwhName) }()

		logger.Info("Running test: webhook config update without matching field changes does not trigger reconciliation")

		ct := createVAPConstraintTemplate("test-ct" + suffix)
		testutils.CreateThenCleanup(ctx, t, c, ct)

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

		time.Sleep(500 * time.Millisecond)
		for len(ctEvents) > 0 {
			<-ctEvents
		}

		err := retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			updatedWebhook := &admissionregistrationv1.ValidatingWebhookConfiguration{}
			if err := c.Get(ctx, types.NamespacedName{Name: webhookName}, updatedWebhook); err != nil {
				return err
			}

			if updatedWebhook.Annotations == nil {
				updatedWebhook.Annotations = make(map[string]string)
			}
			updatedWebhook.Annotations["test-annotation"] = "test-value"

			return c.Update(ctx, updatedWebhook)
		})
		require.NoError(t, err)

		time.Sleep(1 * time.Second)
		require.Empty(t, ctEvents, "no CT reconciliation events should be triggered for non-matching field changes")
	})

	t.Run("webhook config deletion triggers CT reconciliation and cache removal", func(t *testing.T) {
		webhookNameMu.Lock()
		defer webhookNameMu.Unlock()

		suffix := "-deleted"
		webhookName := testVwhName + suffix

		setVwhName(webhookName)
		defer func() { setVwhName(testVwhName) }()

		logger.Info("Running test: webhook config deletion triggers CT reconciliation and cache removal")

		ct := createVAPConstraintTemplate("test-ct" + suffix)
		testutils.CreateThenCleanup(ctx, t, c, ct)

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

		time.Sleep(500 * time.Millisecond)
		for len(ctEvents) > 0 {
			<-ctEvents
		}

		err = c.Delete(ctx, vwh)
		require.NoError(t, err)

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
			return true
		}, func() error {
			if len(ctEvents) == 0 {
				return fmt.Errorf("no CT reconciliation events received after deletion")
			}
			return nil
		})
		require.NoError(t, err, "CT reconciliation should be triggered after webhook deletion")

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
		webhookNameMu.Lock()
		defer webhookNameMu.Unlock()

		suffix := "-namespace-selector"
		webhookName := testVwhName + suffix

		setVwhName(webhookName)
		defer func() { setVwhName(testVwhName) }()

		logger.Info("Running test: webhook namespace selector change triggers reconciliation")

		ct := createVAPConstraintTemplate("test-ct" + suffix)
		testutils.CreateThenCleanup(ctx, t, c, ct)

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

		time.Sleep(500 * time.Millisecond)
		for len(ctEvents) > 0 {
			<-ctEvents
		}
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

func TestTriggerConstraintTemplateReconciliation(t *testing.T) {
	if !operations.IsAssigned(operations.Generate) {
		t.Skip("Skipping test because Generate operation is not assigned")
	}

	originalVwhName := getVwhName()
	defer func() { setVwhName(originalVwhName) }()

	setVwhName(testVwhName)

	mgr, _ := testutils.SetupManager(t, cfg)
	c := testclient.NewRetryClient(mgr.GetClient())

	ctx := context.Background()
	testutils.StartManager(ctx, t, mgr)

	t.Run("successful reconciliation sends events for VAP-enabled templates", func(t *testing.T) {
		logger.Info("Running test: successful reconciliation sends events for VAP-enabled templates")

		ct1 := createVAPConstraintTemplate("trigger-test-ct1")
		ct2 := createVAPConstraintTemplate("trigger-test-ct2")
		testutils.CreateThenCleanup(ctx, t, c, ct1)
		testutils.CreateThenCleanup(ctx, t, c, ct2)

		ctEvents := make(chan event.GenericEvent, 10)

		reconciler := &ReconcileWebhookConfig{
			Client:   c,
			scheme:   mgr.GetScheme(),
			ctEvents: ctEvents,
		}

		err := reconciler.triggerConstraintTemplateReconciliation(ctx)
		require.NoError(t, err, "Should trigger ConstraintTemplate reonciliation without error")

		err = retry.OnError(testutils.ConstantRetry, func(_ error) bool {
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

		ct := createNonVAPConstraintTemplate("trigger-test-non-vap")
		testutils.CreateThenCleanup(ctx, t, c, ct)

		ctEvents := make(chan event.GenericEvent, 10)

		reconciler := &ReconcileWebhookConfig{
			Client:   c,
			scheme:   mgr.GetScheme(),
			ctEvents: ctEvents,
		}

		err := reconciler.triggerConstraintTemplateReconciliation(ctx)
		require.NoError(t, err, "Should trigger ConstraintTemplate reonciliation without error")

		time.Sleep(500 * time.Millisecond)
	})

	t.Run("handles nil ctEvents channel gracefully", func(t *testing.T) {
		logger.Info("Running test: handles nil ctEvents channel gracefully")

		reconciler := &ReconcileWebhookConfig{
			Client:   c,
			scheme:   mgr.GetScheme(),
			ctEvents: nil,
		}

		err := reconciler.triggerConstraintTemplateReconciliation(ctx)
		require.NoError(t, err, "Should trigger ConstraintTemplate reonciliation without error")
	})
}

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

func TestSendEventWithRetry(t *testing.T) {
	tests := []struct {
		name          string
		channelSize   int
		expectSuccess bool
		expectRetries bool
		cancelContext bool
		cancelAfter   time.Duration
		validateDelay bool
	}{
		{
			name:          "successful send on first attempt",
			channelSize:   10,
			expectSuccess: true,
			expectRetries: false,
		},
		{
			name:          "successful send after retries",
			channelSize:   0,
			expectSuccess: true,
			expectRetries: true,
		},
		{
			name:          "context cancellation during retry",
			channelSize:   0,
			cancelContext: true,
			cancelAfter:   50 * time.Millisecond,
			expectSuccess: false,
		},
		{
			name:          "max retries exceeded",
			channelSize:   0,
			expectSuccess: false,
			expectRetries: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctEvents := make(chan event.GenericEvent, tc.channelSize)

			template := &v1beta1.ConstraintTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-template",
				},
			}

			r := &ReconcileWebhookConfig{
				ctEvents: ctEvents,
			}

			ctx := context.Background()
			if tc.cancelContext {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.cancelAfter)
				defer cancel()
			}

			if tc.expectRetries && tc.expectSuccess && !tc.cancelContext {
				go func() {
					time.Sleep(150 * time.Millisecond)
					select {
					case <-ctEvents:
					case <-time.After(1 * time.Second):
					}
				}()
			}

			startTime := time.Now()

			err := r.sendEventWithRetry(ctx, template)

			switch {
			case tc.cancelContext:
				require.Error(t, err, "expected context cancellation error")
				require.Equal(t, context.DeadlineExceeded, err, "expected deadline exceeded error")
			case tc.name == "max retries exceeded":
				require.Error(t, err, "expected error when max retries exceeded")
			default:
				require.NoError(t, err, "sendEventWithRetry should not return error")
			}

			if tc.expectRetries && tc.validateDelay {
				elapsed := time.Since(startTime)
				require.True(t, elapsed > 50*time.Millisecond, "expected some delay for retries")
			}

			if tc.expectSuccess && !tc.cancelContext {
				select {
				case receivedEvent := <-ctEvents:
					require.Equal(t, template, receivedEvent.Object, "received event should match sent template")
				case <-time.After(100 * time.Millisecond):
					if tc.channelSize > 0 {
						t.Fatal("expected event to be received")
					}
				}
			}
		})
	}
}

func TestSendEventWithRetry_ChannelBackpressure(t *testing.T) {
	t.Run("channel becomes available after retry", func(t *testing.T) {
		ctEvents := make(chan event.GenericEvent, 1)
		template := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template"},
		}

		r := &ReconcileWebhookConfig{ctEvents: ctEvents}

		ctEvents <- event.GenericEvent{Object: template}

		go func() {
			time.Sleep(50 * time.Millisecond)
			select {
			case <-ctEvents:
			default:
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		startTime := time.Now()
		err := r.sendEventWithRetry(ctx, template)
		elapsed := time.Since(startTime)

		require.NoError(t, err, "should succeed when channel becomes available")
		require.True(t, elapsed > 25*time.Millisecond, "should have taken time for retry")

		select {
		case receivedEvent := <-ctEvents:
			require.Equal(t, template, receivedEvent.Object)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("expected event to be received")
		}
	})
}

func TestDirtyTemplateManagement(t *testing.T) {
	t.Run("markDirtyTemplate adds template to dirty state", func(t *testing.T) {
		r := &ReconcileWebhookConfig{}

		template := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template"},
		}

		r.markDirtyTemplate(template)

		r.dirtyMu.Lock()
		defer r.dirtyMu.Unlock()
		require.NotNil(t, r.dirtyTemplates)
		require.Contains(t, r.dirtyTemplates, "test-template")
		require.Equal(t, template, r.dirtyTemplates["test-template"])
	})

	t.Run("markDirtyTemplate handles multiple templates", func(t *testing.T) {
		r := &ReconcileWebhookConfig{}

		template1 := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "template-1"},
		}
		template2 := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "template-2"},
		}

		r.markDirtyTemplate(template1)
		r.markDirtyTemplate(template2)

		r.dirtyMu.Lock()
		defer r.dirtyMu.Unlock()
		require.Len(t, r.dirtyTemplates, 2)
		require.Contains(t, r.dirtyTemplates, "template-1")
		require.Contains(t, r.dirtyTemplates, "template-2")
	})

	t.Run("getDirtyTemplatesAndClear returns all dirty templates and clears state", func(t *testing.T) {
		r := &ReconcileWebhookConfig{}

		template1 := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "template-1"},
		}
		template2 := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "template-2"},
		}

		r.markDirtyTemplate(template1)
		r.markDirtyTemplate(template2)

		dirtyTemplates := r.getDirtyTemplatesAndClear()

		require.Len(t, dirtyTemplates, 2)
		templateNames := make([]string, len(dirtyTemplates))
		for i, template := range dirtyTemplates {
			templateNames[i] = template.Name
		}
		require.Contains(t, templateNames, "template-1")
		require.Contains(t, templateNames, "template-2")

		r.dirtyMu.Lock()
		defer r.dirtyMu.Unlock()
		require.Empty(t, r.dirtyTemplates)
	})

	t.Run("getDirtyTemplatesAndClear returns nil when no dirty templates", func(t *testing.T) {
		r := &ReconcileWebhookConfig{}

		dirtyTemplates := r.getDirtyTemplatesAndClear()

		require.Nil(t, dirtyTemplates)
	})
}

func TestTriggerDirtyTemplateReconciliation(t *testing.T) {
	t.Run("processes dirty templates and sends events", func(t *testing.T) {
		ctEvents := make(chan event.GenericEvent, 10)
		r := &ReconcileWebhookConfig{ctEvents: ctEvents}

		template1 := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "template-1"},
		}
		template2 := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "template-2"},
		}

		r.markDirtyTemplate(template1)
		r.markDirtyTemplate(template2)

		ctx := context.Background()
		err := r.triggerDirtyTemplateReconciliation(ctx)

		require.NoError(t, err)
		require.Len(t, ctEvents, 2)

		receivedEvents := make([]event.GenericEvent, 2)
		receivedEvents[0] = <-ctEvents
		receivedEvents[1] = <-ctEvents

		receivedNames := make([]string, 2)
		for i, evt := range receivedEvents {
			if template, ok := evt.Object.(*v1beta1.ConstraintTemplate); ok {
				receivedNames[i] = template.Name
			}
		}
		require.Contains(t, receivedNames, "template-1")
		require.Contains(t, receivedNames, "template-2")

		r.dirtyMu.Lock()
		defer r.dirtyMu.Unlock()
		require.Empty(t, r.dirtyTemplates)
	})

	t.Run("re-marks failed templates as dirty", func(t *testing.T) {
		ctEvents := make(chan event.GenericEvent)
		r := &ReconcileWebhookConfig{ctEvents: ctEvents}

		template := &v1beta1.ConstraintTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "test-template"},
		}

		r.markDirtyTemplate(template)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err := r.triggerDirtyTemplateReconciliation(ctx)

		require.Error(t, err)

		r.dirtyMu.Lock()
		defer r.dirtyMu.Unlock()
		require.Contains(t, r.dirtyTemplates, "test-template")
	})

	t.Run("returns nil when no dirty templates", func(t *testing.T) {
		r := &ReconcileWebhookConfig{}

		ctx := context.Background()
		err := r.triggerDirtyTemplateReconciliation(ctx)

		require.NoError(t, err)
	})
}
