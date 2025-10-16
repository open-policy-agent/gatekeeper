package webhookconfigcache

import (
	"reflect"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestUpdateConfigStoresAndRetrieves(t *testing.T) {
	cache := NewWebhookConfigCache(nil)

	config := sampleConfig()

	if updated := cache.UpdateConfig("test-webhook", config); !updated {
		t.Fatalf("expected first update to report a change")
	}

	stored, ok := cache.GetConfig("test-webhook")
	if !ok {
		t.Fatalf("expected config to be present in cache")
	}

	if !reflect.DeepEqual(stored, config) {
		t.Fatalf("stored config mismatch\nwant: %#v\n got: %#v", config, stored)
	}
}

func TestUpdateConfigDetectsChanges(t *testing.T) {
	cache := NewWebhookConfigCache(nil)

	config := sampleConfig()
	cache.UpdateConfig("test-webhook", config)

	if updated := cache.UpdateConfig("test-webhook", config); updated {
		t.Fatalf("expected identical config to not report a change")
	}

	modified := sampleConfig()
	policy := admissionregistrationv1.Equivalent
	modified.MatchPolicy = &policy
	modified.MatchConditions = append(modified.MatchConditions, admissionregistrationv1.MatchCondition{
		Name:       "extra-condition",
		Expression: "request.operation == 'UPDATE'",
	})

	if updated := cache.UpdateConfig("test-webhook", modified); !updated {
		t.Fatalf("expected modified config to report a change")
	}

	stored, ok := cache.GetConfig("test-webhook")
	if !ok {
		t.Fatalf("expected config to remain in cache")
	}

	if !reflect.DeepEqual(stored, modified) {
		t.Fatalf("stored config mismatch after modification\nwant: %#v\n got: %#v", modified, stored)
	}
}

func TestRemoveConfig(t *testing.T) {
	cache := NewWebhookConfigCache(nil)

	cache.UpdateConfig("test-webhook", sampleConfig())

	cache.RemoveConfig("test-webhook")

	if _, ok := cache.GetConfig("test-webhook"); ok {
		t.Fatalf("expected config to be removed from cache")
	}
}

func sampleConfig() WebhookMatchingConfig {
	policy := admissionregistrationv1.Exact

	return WebhookMatchingConfig{
		NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"gatekeeper": "enabled"}},
		ObjectSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: "environment", Operator: metav1.LabelSelectorOpIn, Values: []string{"prod", "staging"}},
		}},
		Rules: []admissionregistrationv1.RuleWithOperations{{
			Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
			Rule: admissionregistrationv1.Rule{
				APIGroups:   []string{"apps"},
				APIVersions: []string{"v1"},
				Resources:   []string{"deployments"},
			},
		}},
		MatchPolicy: &policy,
		MatchConditions: []admissionregistrationv1.MatchCondition{{
			Name:       "is-create",
			Expression: "request.operation == 'CREATE'",
		}},
	}
}
