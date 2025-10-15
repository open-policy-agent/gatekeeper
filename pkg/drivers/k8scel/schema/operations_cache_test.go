package schema

import (
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestNewWebhookOperationsCache(t *testing.T) {
	cache := NewWebhookOperationsCache()
	if cache == nil {
		t.Fatal("NewWebhookOperationsCache() returned nil")
	}
	if cache.operations == nil {
		t.Error("operations map should be initialized")
	}
	if cache.resourceLevelOperations == nil {
		t.Error("resourceLevelOperations map should be initialized")
	}
	if len(cache.operations) != 0 {
		t.Errorf("operations map should be empty, got %d items", len(cache.operations))
	}
}

func TestWebhookOperationsCache_HasOperation(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*WebhookOperationsCache)
		operation admissionregistrationv1.OperationType
		want      bool
	}{
		{
			name: "nil operations map",
			setup: func(c *WebhookOperationsCache) {
				c.operations = nil
			},
			operation: admissionregistrationv1.Create,
			want:      false,
		},
		{
			name: "operation exists and is true",
			setup: func(c *WebhookOperationsCache) {
				c.operations[admissionregistrationv1.Create] = true
			},
			operation: admissionregistrationv1.Create,
			want:      true,
		},
		{
			name: "operation exists but is false",
			setup: func(c *WebhookOperationsCache) {
				c.operations[admissionregistrationv1.Create] = false
			},
			operation: admissionregistrationv1.Create,
			want:      false,
		},
		{
			name: "operation does not exist",
			setup: func(c *WebhookOperationsCache) {
				c.operations[admissionregistrationv1.Create] = true
			},
			operation: admissionregistrationv1.Update,
			want:      false,
		},
		{
			name: "OperationAll is enabled",
			setup: func(c *WebhookOperationsCache) {
				c.operations[admissionregistrationv1.OperationAll] = true
			},
			operation: admissionregistrationv1.Create,
			want:      true,
		},
		{
			name: "OperationAll is false",
			setup: func(c *WebhookOperationsCache) {
				c.operations[admissionregistrationv1.OperationAll] = false
			},
			operation: admissionregistrationv1.Create,
			want:      false,
		},
		{
			name: "specific operation takes precedence over OperationAll",
			setup: func(c *WebhookOperationsCache) {
				c.operations[admissionregistrationv1.Create] = true
				c.operations[admissionregistrationv1.OperationAll] = true
			},
			operation: admissionregistrationv1.Create,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewWebhookOperationsCache()
			if tt.setup != nil {
				tt.setup(cache)
			}
			got := cache.HasOperation(tt.operation)
			if got != tt.want {
				t.Errorf("HasOperation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWebhookOperationsCache_GetAllOperations(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*WebhookOperationsCache)
		want  []admissionregistrationv1.OperationType
	}{
		{
			name:  "empty cache",
			setup: func(c *WebhookOperationsCache) {},
			want:  []admissionregistrationv1.OperationType{},
		},
		{
			name: "single operation",
			setup: func(c *WebhookOperationsCache) {
				c.operations[admissionregistrationv1.Create] = true
			},
			want: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
		},
		{
			name: "multiple operations",
			setup: func(c *WebhookOperationsCache) {
				c.operations[admissionregistrationv1.Create] = true
				c.operations[admissionregistrationv1.Update] = true
				c.operations[admissionregistrationv1.Delete] = true
			},
			want: []admissionregistrationv1.OperationType{
				admissionregistrationv1.Create,
				admissionregistrationv1.Update,
				admissionregistrationv1.Delete,
			},
		},
		{
			name: "OperationAll returns only OperationAll",
			setup: func(c *WebhookOperationsCache) {
				c.operations[admissionregistrationv1.Create] = true
				c.operations[admissionregistrationv1.Update] = true
				c.operations[admissionregistrationv1.OperationAll] = true
			},
			want: []admissionregistrationv1.OperationType{admissionregistrationv1.OperationAll},
		},
		{
			name: "false operations are excluded",
			setup: func(c *WebhookOperationsCache) {
				c.operations[admissionregistrationv1.Create] = true
				c.operations[admissionregistrationv1.Update] = false
				c.operations[admissionregistrationv1.Delete] = true
			},
			want: []admissionregistrationv1.OperationType{
				admissionregistrationv1.Create,
				admissionregistrationv1.Delete,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewWebhookOperationsCache()
			if tt.setup != nil {
				tt.setup(cache)
			}
			got := cache.GetAllOperations()
			gotSet := sets.NewString(stringSliceFromOps(got)...)
			wantSet := sets.NewString(stringSliceFromOps(tt.want)...)
			if !gotSet.Equal(wantSet) {
				t.Errorf("GetAllOperations() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWebhookOperationsCache_ClearAllOperations(t *testing.T) {
	cache := NewWebhookOperationsCache()
	cache.operations[admissionregistrationv1.Create] = true
	cache.operations[admissionregistrationv1.Update] = true
	cache.resourceLevelOperations["pods"] = map[admissionregistrationv1.OperationType]bool{
		admissionregistrationv1.Create: true,
	}

	cache.ClearAllOperations()

	if len(cache.operations) != 0 {
		t.Errorf("operations map should be empty after clear, got %d items", len(cache.operations))
	}
	if len(cache.resourceLevelOperations) != 0 {
		t.Errorf("resourceLevelOperations map should be empty after clear, got %d items", len(cache.resourceLevelOperations))
	}
}

func TestWebhookOperationsCache_extractOperationsFromWebhook(t *testing.T) {
	tests := []struct {
		name            string
		webhook         *admissionregistrationv1.ValidatingWebhook
		existingOps     map[admissionregistrationv1.OperationType]bool
		wantHasDiff     bool
		wantOperations  []admissionregistrationv1.OperationType
		wantResourceOps map[string][]admissionregistrationv1.OperationType
	}{
		{
			name: "new operations added",
			webhook: &admissionregistrationv1.ValidatingWebhook{
				Name: "test-webhook",
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
							admissionregistrationv1.Update,
						},
						Rule: admissionregistrationv1.Rule{
							Resources: []string{"pods", "deployments"},
						},
					},
				},
			},
			existingOps: nil,
			wantHasDiff: true,
			wantOperations: []admissionregistrationv1.OperationType{
				admissionregistrationv1.Create,
				admissionregistrationv1.Update,
			},
			wantResourceOps: map[string][]admissionregistrationv1.OperationType{
				"pods": {
					admissionregistrationv1.Create,
					admissionregistrationv1.Update,
				},
				"deployments": {
					admissionregistrationv1.Create,
					admissionregistrationv1.Update,
				},
			},
		},
		{
			name: "no new operations",
			webhook: &admissionregistrationv1.ValidatingWebhook{
				Name: "test-webhook",
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
						},
						Rule: admissionregistrationv1.Rule{
							Resources: []string{"pods"},
						},
					},
				},
			},
			existingOps: map[admissionregistrationv1.OperationType]bool{
				admissionregistrationv1.Create: true,
			},
			wantHasDiff: false,
			wantOperations: []admissionregistrationv1.OperationType{
				admissionregistrationv1.Create,
			},
			wantResourceOps: map[string][]admissionregistrationv1.OperationType{
				"pods": {
					admissionregistrationv1.Create,
				},
			},
		},
		{
			name: "multiple rules",
			webhook: &admissionregistrationv1.ValidatingWebhook{
				Name: "test-webhook",
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
						},
						Rule: admissionregistrationv1.Rule{
							Resources: []string{"pods"},
						},
					},
					{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Update,
							admissionregistrationv1.Delete,
						},
						Rule: admissionregistrationv1.Rule{
							Resources: []string{"services"},
						},
					},
				},
			},
			existingOps: nil,
			wantHasDiff: true,
			wantOperations: []admissionregistrationv1.OperationType{
				admissionregistrationv1.Create,
				admissionregistrationv1.Update,
				admissionregistrationv1.Delete,
			},
			wantResourceOps: map[string][]admissionregistrationv1.OperationType{
				"pods": {
					admissionregistrationv1.Create,
				},
				"services": {
					admissionregistrationv1.Update,
					admissionregistrationv1.Delete,
				},
			},
		},
		{
			name: "empty webhook",
			webhook: &admissionregistrationv1.ValidatingWebhook{
				Name:  "empty-webhook",
				Rules: []admissionregistrationv1.RuleWithOperations{},
			},
			existingOps:     nil,
			wantHasDiff:     false,
			wantOperations:  []admissionregistrationv1.OperationType{},
			wantResourceOps: map[string][]admissionregistrationv1.OperationType{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewWebhookOperationsCache()
			if tt.existingOps != nil {
				cache.operations = tt.existingOps
			}

			gotHasDiff := cache.extractOperationsFromWebhook(tt.webhook)

			if gotHasDiff != tt.wantHasDiff {
				t.Errorf("extractOperationsFromWebhook() hasDiff = %v, want %v", gotHasDiff, tt.wantHasDiff)
			}

			// Check operations
			gotOps := []admissionregistrationv1.OperationType{}
			for op, exists := range cache.operations {
				if exists {
					gotOps = append(gotOps, op)
				}
			}
			gotOpsSet := sets.NewString(stringSliceFromOps(gotOps)...)
			wantOpsSet := sets.NewString(stringSliceFromOps(tt.wantOperations)...)
			if !gotOpsSet.Equal(wantOpsSet) {
				t.Errorf("operations = %v, want %v", gotOps, tt.wantOperations)
			}

			// Check resource level operations
			for resource, wantOps := range tt.wantResourceOps {
				gotResourceOps, exists := cache.resourceLevelOperations[resource]
				if !exists {
					t.Errorf("resource %s not found in resourceLevelOperations", resource)
					continue
				}
				gotResourceOpsList := []admissionregistrationv1.OperationType{}
				for op, exists := range gotResourceOps {
					if exists {
						gotResourceOpsList = append(gotResourceOpsList, op)
					}
				}
				gotResOpsSet := sets.NewString(stringSliceFromOps(gotResourceOpsList)...)
				wantResOpsSet := sets.NewString(stringSliceFromOps(wantOps)...)
				if !gotResOpsSet.Equal(wantResOpsSet) {
					t.Errorf("resource %s operations = %v, want %v", resource, gotResourceOpsList, wantOps)
				}
			}
		})
	}
}

func TestWebhookOperationsCache_ExtractOperationsFromWebhookConfiguration(t *testing.T) {
	tests := []struct {
		name           string
		vwhc           *admissionregistrationv1.ValidatingWebhookConfiguration
		existingOps    map[admissionregistrationv1.OperationType]bool
		wantIsChanged  bool
		wantOperations []admissionregistrationv1.OperationType
	}{
		{
			name: "new configuration with operations",
			vwhc: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-vwhc",
				},
				Webhooks: []admissionregistrationv1.ValidatingWebhook{
					{
						Name: "webhook1",
						Rules: []admissionregistrationv1.RuleWithOperations{
							{
								Operations: []admissionregistrationv1.OperationType{
									admissionregistrationv1.Create,
									admissionregistrationv1.Update,
								},
								Rule: admissionregistrationv1.Rule{
									Resources: []string{"pods"},
								},
							},
						},
					},
				},
			},
			existingOps:   nil,
			wantIsChanged: true,
			wantOperations: []admissionregistrationv1.OperationType{
				admissionregistrationv1.Create,
				admissionregistrationv1.Update,
			},
		},
		{
			name: "same configuration - still returns true due to reset",
			vwhc: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-vwhc",
				},
				Webhooks: []admissionregistrationv1.ValidatingWebhook{
					{
						Name: "webhook1",
						Rules: []admissionregistrationv1.RuleWithOperations{
							{
								Operations: []admissionregistrationv1.OperationType{
									admissionregistrationv1.Create,
								},
								Rule: admissionregistrationv1.Rule{
									Resources: []string{"pods"},
								},
							},
						},
					},
				},
			},
			existingOps: map[admissionregistrationv1.OperationType]bool{
				admissionregistrationv1.Create: true,
			},
			wantIsChanged: true, // Changed from false to true due to reset logic
			wantOperations: []admissionregistrationv1.OperationType{
				admissionregistrationv1.Create,
			},
		},
		{
			name: "multiple webhooks",
			vwhc: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-vwhc",
				},
				Webhooks: []admissionregistrationv1.ValidatingWebhook{
					{
						Name: "webhook1",
						Rules: []admissionregistrationv1.RuleWithOperations{
							{
								Operations: []admissionregistrationv1.OperationType{
									admissionregistrationv1.Create,
								},
								Rule: admissionregistrationv1.Rule{
									Resources: []string{"pods"},
								},
							},
						},
					},
					{
						Name: "webhook2",
						Rules: []admissionregistrationv1.RuleWithOperations{
							{
								Operations: []admissionregistrationv1.OperationType{
									admissionregistrationv1.Update,
									admissionregistrationv1.Delete,
								},
								Rule: admissionregistrationv1.Rule{
									Resources: []string{"services"},
								},
							},
						},
					},
				},
			},
			existingOps:   nil,
			wantIsChanged: true,
			wantOperations: []admissionregistrationv1.OperationType{
				admissionregistrationv1.Create,
				admissionregistrationv1.Update,
				admissionregistrationv1.Delete,
			},
		},
		{
			name: "empty webhook configuration",
			vwhc: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "empty-vwhc",
				},
				Webhooks: []admissionregistrationv1.ValidatingWebhook{},
			},
			existingOps:    nil,
			wantIsChanged:  false,
			wantOperations: []admissionregistrationv1.OperationType{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewWebhookOperationsCache()
			if tt.existingOps != nil {
				cache.operations = tt.existingOps
			}

			gotIsChanged := cache.ExtractOperationsFromWebhookConfiguration(tt.vwhc)

			if gotIsChanged != tt.wantIsChanged {
				t.Errorf("ExtractOperationsFromWebhookConfiguration() isChanged = %v, want %v (webhooks: %d)",
					gotIsChanged, tt.wantIsChanged, len(tt.vwhc.Webhooks))
			}

			// Check operations
			gotOps := []admissionregistrationv1.OperationType{}
			for op, exists := range cache.operations {
				if exists {
					gotOps = append(gotOps, op)
				}
			}
			gotOpsSet := sets.NewString(stringSliceFromOps(gotOps)...)
			wantOpsSet := sets.NewString(stringSliceFromOps(tt.wantOperations)...)
			if !gotOpsSet.Equal(wantOpsSet) {
				t.Errorf("operations = %v, want %v", gotOps, tt.wantOperations)
			}
		})
	}
}

func TestWebhookOperationsCache_Concurrency(t *testing.T) {
	// Test concurrent access to ensure proper locking
	cache := NewWebhookOperationsCache()
	cache.operations[admissionregistrationv1.Create] = true

	done := make(chan bool)

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			cache.HasOperation(admissionregistrationv1.Create)
			cache.GetAllOperations()
			done <- true
		}()
	}

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func(idx int) {
			vwhc := &admissionregistrationv1.ValidatingWebhookConfiguration{
				Webhooks: []admissionregistrationv1.ValidatingWebhook{
					{
						Rules: []admissionregistrationv1.RuleWithOperations{
							{
								Operations: []admissionregistrationv1.OperationType{
									admissionregistrationv1.Update,
								},
								Rule: admissionregistrationv1.Rule{
									Resources: []string{"pods"},
								},
							},
						},
					},
				},
			}
			cache.ExtractOperationsFromWebhookConfiguration(vwhc)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}

	// Verify final state is consistent
	if !cache.HasOperation(admissionregistrationv1.Create) {
		t.Error("Create operation should still exist")
	}
}

func TestResetLogic(t *testing.T) {
	cache := NewWebhookOperationsCache()

	// Set an existing operation
	cache.operations[admissionregistrationv1.Create] = true
	t.Logf("Before call: operations = %v", cache.operations)

	// Create a simple vwhc
	vwhc := &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			{
				Name: "test-webhook",
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
						},
						Rule: admissionregistrationv1.Rule{
							Resources: []string{"pods"},
						},
					},
				},
			},
		},
	}

	t.Logf("VWHC webhooks: %d", len(vwhc.Webhooks))
	t.Logf("VWHC webhook[0] rules: %d", len(vwhc.Webhooks[0].Rules))
	isChanged := cache.ExtractOperationsFromWebhookConfiguration(vwhc)

	t.Logf("After call: operations = %v", cache.operations)
	t.Logf("isChanged = %v", isChanged)
	if !isChanged {
		t.Error("Expected isChanged = true after reset and re-extraction, got false")
	}

	if !cache.HasOperation(admissionregistrationv1.Create) {
		t.Error("Expected Create operation to be in cache after extraction")
	}
}