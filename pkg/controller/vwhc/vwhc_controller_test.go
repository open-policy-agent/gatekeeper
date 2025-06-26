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

package vwhc

import (
	"context"
	"errors"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/webhook"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	MockGatekeeperVWHCName = "gatekeeper-validating-webhook-configuration"
)

func resetGlobal() {
	EnableDeleteOpsInVwhc = nil
}

func mustInitializeScheme(scheme *runtime.Scheme) *runtime.Scheme {
	if err := admissionregistrationv1.AddToScheme(scheme); err != nil {
		panic(err)
	}

	return scheme
}

func TestReconcile(t *testing.T) {

	ctx := context.TODO()
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: MockGatekeeperVWHCName,
		},
	}
	EnableDeleteOpsInVwhc = ptr.To[bool](false)
	tests := []struct {
		name             string
		existingVWHC     *admissionregistrationv1.ValidatingWebhookConfiguration
		getError         error
		expectedResult   reconcile.Result
		expectedRequeue  bool
		expectedEnable   bool
		expectLogContain string
	}{
		{
			name:           "not found",
			expectedResult: reconcile.Result{},
			expectedEnable: false,
		},
		{
			name: "without delete operations",
			existingVWHC: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: MockGatekeeperVWHCName,
				},
				TypeMeta: metav1.TypeMeta{
					Kind:       "ValidatingWebhookConfiguration",
					APIVersion: "admissionregistration.k8s.io/v1",
				},
				Webhooks: []admissionregistrationv1.ValidatingWebhook{
					{
						Rules: []admissionregistrationv1.RuleWithOperations{
							{
								Operations: []admissionregistrationv1.OperationType{
									admissionregistrationv1.Create,
									admissionregistrationv1.Update,
								},
							},
						},
					},
				},
			},
			expectedResult: reconcile.Result{},
			expectedEnable: false,
		},
		{
			name: "with delete operation",
			existingVWHC: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: MockGatekeeperVWHCName,
				},
				Webhooks: []admissionregistrationv1.ValidatingWebhook{
					{
						Rules: []admissionregistrationv1.RuleWithOperations{
							{
								Operations: []admissionregistrationv1.OperationType{
									admissionregistrationv1.Delete,
								},
							},
						},
					},
				},
			},
			expectedResult: reconcile.Result{},
			expectedEnable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer resetGlobal()

			var initObjs []runtime.Object
			if tt.existingVWHC != nil {
				initObjs = append(initObjs, tt.existingVWHC)
			}

			fakeClient := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(initObjs...).Build()

			r := &ReconcileVWHC{
				reader: fakeClient,
			}

			result, err := r.Reconcile(ctx, req)

			if tt.getError != nil && !errors.Is(err, tt.getError) {
				t.Errorf("expected error %v, got %v", tt.getError, err)
			}

			if result != tt.expectedResult {
				t.Errorf("expected result %+v, got %+v", tt.expectedResult, result)
			}

			if EnableDeleteOpsInVwhc == nil {
				t.Errorf("EnableDeleteOpsInVwhc should not be nil")
			} else if *EnableDeleteOpsInVwhc != tt.expectedEnable {
				t.Errorf("expected EnableDeleteOpsInVwhc=%v, got %v", tt.expectedEnable, *EnableDeleteOpsInVwhc)
			}
		})
	}
}

func TestReconcileWebhookMapFunc(t *testing.T) {
	tests := []struct {
		name     string
		obj      *admissionregistrationv1.ValidatingWebhookConfiguration
		expected []reconcile.Request
	}{
		{
			name: "name not match",
			obj: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "wrong-name",
				},
			},
			expected: nil,
		},
		{
			name: "no gatekeeper label",
			obj: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:   *webhook.VwhName,
					Labels: map[string]string{},
				},
			},
			expected: nil,
		},
		{
			name: "label value not yes",
			obj: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: *webhook.VwhName,
					Labels: map[string]string{
						GatekeeperWebhookLabel: "no",
					},
				},
			},
			expected: nil,
		},
		{
			name: "valid gatekeeper webhook config",
			obj: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      *webhook.VwhName,
					Namespace: "gatekeeper-system",
					Labels: map[string]string{
						GatekeeperWebhookLabel: "yes",
					},
				},
			},
			expected: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Namespace: "gatekeeper-system",
						Name:      *webhook.VwhName,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TODO()
			mapper := reconcileWebhookMapFunc()
			reqs := mapper(ctx, tt.obj)

			if len(reqs) != len(tt.expected) {
				t.Errorf("expected %d requests, got %d", len(tt.expected), len(reqs))
				return
			}

			for i := range reqs {
				if reqs[i].NamespacedName != tt.expected[i].NamespacedName {
					t.Errorf("expected request %v, got %v", tt.expected[i].NamespacedName, reqs[i].NamespacedName)
				}
			}
		})
	}
}
