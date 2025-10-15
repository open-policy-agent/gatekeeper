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

package schema

import (
	"sync"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
)

// WebhookOperationsCache stores webhook operations for efficient access
type WebhookOperationsCache struct {
	operations              map[admissionregistrationv1.OperationType]bool
	resourceLevelOperations map[string]map[admissionregistrationv1.OperationType]bool
	mutex                   sync.RWMutex
}

// NewWebhookOperationsCache creates a new webhook operations cache
func NewWebhookOperationsCache() *WebhookOperationsCache {
	return &WebhookOperationsCache{
		operations:              make(map[admissionregistrationv1.OperationType]bool),
		resourceLevelOperations: make(map[string]map[admissionregistrationv1.OperationType]bool),
	}
}

// HasOperation checks if a specific operation is enabled for a webhook
func (c *WebhookOperationsCache) HasOperation(operation admissionregistrationv1.OperationType) bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if c.operations == nil {
		return false
	}
	if exist, ok := c.operations[operation]; ok && exist {
		return true
	}
	// Check if OperationAll is enabled
	if exist, ok := c.operations[admissionregistrationv1.OperationAll]; ok && exist {
		return true
	}
	return false
}

// GetAllOperations returns all operations across all webhooks
func (c *WebhookOperationsCache) GetAllOperations() []admissionregistrationv1.OperationType {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	result := make([]admissionregistrationv1.OperationType, 0)
	for ops, exist := range c.operations {
		if ops == admissionregistrationv1.OperationAll {
			return []admissionregistrationv1.OperationType{admissionregistrationv1.OperationAll}
		}
		if exist {
			result = append(result, ops)
		}
	}
	return result
}

// ClearAllOperations removes all operations
func (c *WebhookOperationsCache) ClearAllOperations() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.operations = make(map[admissionregistrationv1.OperationType]bool)
	c.resourceLevelOperations = make(map[string]map[admissionregistrationv1.OperationType]bool)
}

// extractOperationsFromWebhook extracts operations from a webhook configuration
func (c *WebhookOperationsCache) extractOperationsFromWebhook(webhook *admissionregistrationv1.ValidatingWebhook) (hasDiff bool) {
	for _, rule := range webhook.Rules {
		for _, op := range rule.Operations {
			if _, exists := c.operations[op]; !exists {
				c.operations[op] = true
				hasDiff = true
			}
		}
		//TODO limit the generated VAP operations to webhook resource level
		for _, resource := range rule.Resources {
			if _, exists := c.resourceLevelOperations[resource]; !exists {
				c.resourceLevelOperations[resource] = make(map[admissionregistrationv1.OperationType]bool)
			}
			for _, op := range rule.Operations {
				if _, exists := c.resourceLevelOperations[resource][op]; !exists {
					c.resourceLevelOperations[resource][op] = true
				}
			}
		}
	}
	return hasDiff
}

// ExtractOperationsFromWebhookConfiguration extracts operations from all webhooks in a configuration
func (c *WebhookOperationsCache) ExtractOperationsFromWebhookConfiguration(vwhc *admissionregistrationv1.ValidatingWebhookConfiguration) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	var isChanged bool
	//reset operation caches
	c.operations = make(map[admissionregistrationv1.OperationType]bool)
	c.resourceLevelOperations = make(map[string]map[admissionregistrationv1.OperationType]bool)
	for i := range vwhc.Webhooks {
		webhook := &vwhc.Webhooks[i]
		hasDiff := c.extractOperationsFromWebhook(webhook)
		if hasDiff {
			isChanged = true
		}
	}
	return isChanged
}
