package webhookconfigcache

import (
	"reflect"
	"sync"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// WebhookMatchingConfig represents the fields that affect resource matching in a webhook.
type WebhookMatchingConfig struct {
	NamespaceSelector *metav1.LabelSelector                        `json:"namespaceSelector,omitempty"`
	ObjectSelector    *metav1.LabelSelector                        `json:"objectSelector,omitempty"`
	Rules             []admissionregistrationv1.RuleWithOperations `json:"rules,omitempty"`
	MatchPolicy       *admissionregistrationv1.MatchPolicyType     `json:"matchPolicy,omitempty"`
	MatchConditions   []admissionregistrationv1.MatchCondition     `json:"matchConditions,omitempty"`
}

// WebhookConfigCache maintains the current state of webhook configurations.
type WebhookConfigCache struct {
	mu      sync.RWMutex
	configs map[string]WebhookMatchingConfig // webhook name -> config
}

// NewWebhookConfigCache creates a new webhook config cache.
func NewWebhookConfigCache(ctEvents <-chan event.GenericEvent) *WebhookConfigCache {
	return &WebhookConfigCache{
		configs: make(map[string]WebhookMatchingConfig),
	}
}

// UpdateConfig updates the cached config and returns whether it changed.
func (w *WebhookConfigCache) UpdateConfig(webhookName string, newConfig WebhookMatchingConfig) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	logger := log.Log.WithName("webhook-config-cache")
	logger.Info("storing webhook config in cache", "key", webhookName, "hasNamespaceSelector", newConfig.NamespaceSelector != nil)

	oldConfig, exists := w.configs[webhookName]
	if !exists || !reflect.DeepEqual(oldConfig, newConfig) {
		w.configs[webhookName] = newConfig
		logger.Info("webhook config stored/updated in cache", "key", webhookName, "cacheSize", len(w.configs))
		return true
	}
	return false
}

// RemoveConfig removes a webhook config from cache.
func (w *WebhookConfigCache) RemoveConfig(webhookName string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.configs, webhookName)
}

// GetConfig retrieves the current webhook configuration from cache.
func (w *WebhookConfigCache) GetConfig(webhookName string) (WebhookMatchingConfig, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	logger := log.Log.WithName("webhook-config-cache")
	logger.Info("retrieving webhook config from cache", "key", webhookName, "cacheSize", len(w.configs))

	config, exists := w.configs[webhookName]
	logger.Info("webhook config lookup result", "key", webhookName, "exists", exists)
	return config, exists
}
