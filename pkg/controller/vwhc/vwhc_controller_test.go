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
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	celSchema "github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/webhook"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TestReconcileVWHC_Reconcile test Reconcile
func TestReconcileVWHC_Reconcile(t *testing.T) {
	originalVwhName := webhook.VwhName
	webhook.VwhName = ptr.To("gatekeeper-webhook")
	defer func() {
		webhook.VwhName = originalVwhName
	}()

	scheme := runtime.NewScheme()
	_ = admissionregistrationv1.AddToScheme(scheme)
	_ = v1beta1.AddToScheme(scheme)

	tests := []struct {
		name            string
		vwhc            *admissionregistrationv1.ValidatingWebhookConfiguration
		existingObjects []client.Object
		wantRequeue     bool
		wantErr         bool
	}{
		{
			name: "non-existent vwhc",
			vwhc: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-existent",
					Namespace: "default",
				},
			},
			wantRequeue: false,
			wantErr:     false,
		},
		{
			name: "no changes",
			vwhc: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gatekeeper-webhook",
					Namespace: "default",
					Labels: map[string]string{
						GatekeeperWebhookLabel: "yes",
					},
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
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&admissionregistrationv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gatekeeper-webhook",
						Namespace: "default",
						Labels: map[string]string{
							GatekeeperWebhookLabel: "yes",
						},
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
								},
							},
						},
					},
				},
			},
			wantRequeue: false,
			wantErr:     false,
		},
		{
			name: "DELETE change",
			vwhc: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gatekeeper-webhook",
					Namespace: "default",
					Labels: map[string]string{
						GatekeeperWebhookLabel: "yes",
					},
				},
				Webhooks: []admissionregistrationv1.ValidatingWebhook{
					{
						Name: "webhook1",
						Rules: []admissionregistrationv1.RuleWithOperations{
							{
								Operations: []admissionregistrationv1.OperationType{
									admissionregistrationv1.Create,
									admissionregistrationv1.Delete,
								},
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				&admissionregistrationv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gatekeeper-webhook",
						Namespace: "default",
						Labels: map[string]string{
							GatekeeperWebhookLabel: "yes",
						},
					},
					Webhooks: []admissionregistrationv1.ValidatingWebhook{
						{
							Name: "webhook1",
							Rules: []admissionregistrationv1.RuleWithOperations{
								{
									Operations: []admissionregistrationv1.OperationType{
										admissionregistrationv1.Create,
										admissionregistrationv1.Delete,
									},
								},
							},
						},
					},
				},
			},
			wantRequeue: false,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientBuilder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.existingObjects != nil {
				clientBuilder = clientBuilder.WithObjects(tt.existingObjects...)
			}

			fakeClient := clientBuilder.Build()

			r := &ReconcileVWHC{
				reader: fakeClient,
				writer: fakeClient,
				scheme: scheme,
			}
			_, err := r.Reconcile(context.Background(), reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.vwhc.Name,
					Namespace: tt.vwhc.Namespace,
				},
			})

			if (err != nil) != tt.wantErr {
				t.Errorf("Reconcile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

// TestReconcileWebhookMapFunc test reconcileWebhookMapFunc
func TestReconcileWebhookMapFunc(t *testing.T) {
	originalVwhName := webhook.VwhName
	webhook.VwhName = ptr.To("gatekeeper-webhook")
	defer func() {
		webhook.VwhName = originalVwhName
	}()

	tests := []struct {
		name    string
		object  *admissionregistrationv1.ValidatingWebhookConfiguration
		wantLen int
	}{
		{
			name: "name not matching",
			object: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "other-webhook",
				},
			},
			wantLen: 0,
		},
		{
			name: "missing label",
			object: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gatekeeper-webhook",
				},
			},
			wantLen: 0,
		},
		{
			name: "invalid label",
			object: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gatekeeper-webhook",
					Labels: map[string]string{
						GatekeeperWebhookLabel: "no",
					},
				},
			},
			wantLen: 0,
		},
		{
			name: "valid Gatekeeper webhook",
			object: &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gatekeeper-webhook",
					Labels: map[string]string{
						GatekeeperWebhookLabel: "yes",
					},
				},
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := reconcileWebhookMapFunc()
			result := fn(context.Background(), tt.object)

			if len(result) != tt.wantLen {
				t.Errorf("reconcileWebhookMapFunc() = %v, want %v", len(result), tt.wantLen)
			}
		})
	}
}

// TestContainsOpsType test containsOpsType
func TestContainsOpsType(t *testing.T) {
	tests := []struct {
		name    string
		ops     []admissionregistrationv1.OperationType
		opsType admissionregistrationv1.OperationType
		want    bool
	}{
		{
			name:    "contain delete",
			ops:     []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Delete},
			opsType: admissionregistrationv1.Delete,
			want:    true,
		},
		{
			name:    "not contain delete",
			ops:     []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
			opsType: admissionregistrationv1.Delete,
			want:    false,
		},
		{
			name:    "empty ops list",
			ops:     []admissionregistrationv1.OperationType{},
			opsType: admissionregistrationv1.Delete,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsOpsType(tt.ops, tt.opsType); got != tt.want {
				t.Errorf("containsOpsType() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestOpsInVwhcHasDiff test OpsInVwhc.HasDiff
func TestOpsInVwhcHasDiff(t *testing.T) {
	tests := []struct {
		name        string
		origin      celSchema.OpsInVwhc
		current     celSchema.OpsInVwhc
		wantDelete  bool
		wantConnect bool
	}{
		{
			name: "no changes",
			origin: celSchema.OpsInVwhc{
				EnableDeleteOpsInVwhc: ptr.To(false),
				EnableConectOpsInVwhc: ptr.To(false),
			},
			current: celSchema.OpsInVwhc{
				EnableDeleteOpsInVwhc: ptr.To(false),
				EnableConectOpsInVwhc: ptr.To(false),
			},
			wantDelete:  false,
			wantConnect: false,
		},
		{
			name: "Delete change",
			origin: celSchema.OpsInVwhc{
				EnableDeleteOpsInVwhc: ptr.To(false),
				EnableConectOpsInVwhc: ptr.To(false),
			},
			current: celSchema.OpsInVwhc{
				EnableDeleteOpsInVwhc: ptr.To(true),
				EnableConectOpsInVwhc: ptr.To(false),
			},
			wantDelete:  true,
			wantConnect: false,
		},
		{
			name: "Connect change",
			origin: celSchema.OpsInVwhc{
				EnableDeleteOpsInVwhc: ptr.To(false),
				EnableConectOpsInVwhc: ptr.To(false),
			},
			current: celSchema.OpsInVwhc{
				EnableDeleteOpsInVwhc: ptr.To(false),
				EnableConectOpsInVwhc: ptr.To(true),
			},
			wantDelete:  false,
			wantConnect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDelete, gotConnect := tt.origin.HasDiff(tt.current)
			if gotDelete != tt.wantDelete {
				t.Errorf("HasDiff() gotDelete = %v, want %v", gotDelete, tt.wantDelete)
			}
			if gotConnect != tt.wantConnect {
				t.Errorf("HasDiff() gotConnect = %v, want %v", gotConnect, tt.wantConnect)
			}
		})
	}
}

// TestReconcileVWHC_updateAllVAPOperations test updateAllVAPOperations
func TestReconcileVWHC_updateAllVAPOperations(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = admissionregistrationv1.AddToScheme(scheme)
	_ = v1beta1.AddToScheme(scheme)

	tests := []struct {
		name            string
		existingObjects []client.Object
		deleteChanged   bool
		connectChanged  bool
		wantErr         bool
	}{
		{
			name: "no matching VAP",
			existingObjects: []client.Object{
				&admissionregistrationv1.ValidatingAdmissionPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-vap",
					},
				},
			},
			deleteChanged:  true,
			connectChanged: false,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientBuilder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.existingObjects != nil {
				clientBuilder = clientBuilder.WithObjects(tt.existingObjects...)
			}

			fakeClient := clientBuilder.Build()
			r := &ReconcileVWHC{
				reader: fakeClient,
				writer: fakeClient,
				scheme: scheme,
			}

			err := r.updateAllVAPOperations(context.Background(), tt.deleteChanged, tt.connectChanged)
			if (err != nil) != tt.wantErr {
				t.Errorf("updateAllVAPOperations() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestReconcileVWHC_getResourceRuleOps test getResourceRuleOps
func TestReconcileVWHC_getResourceRuleOps(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = admissionregistrationv1.AddToScheme(scheme)
	_ = v1beta1.AddToScheme(scheme)

	source := &celSchema.Source{}
	source.GenerateVAP = ptr.To(true)
	source.ResourceOperations = []admissionregistrationv1.OperationType{
		admissionregistrationv1.Create,
		admissionregistrationv1.Update,
		admissionregistrationv1.Delete,
	}

	ct := &v1beta1.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-template",
		},
		Spec: v1beta1.ConstraintTemplateSpec{
			CRD: v1beta1.CRD{
				Spec: v1beta1.CRDSpec{
					Names: v1beta1.Names{
						Kind: "TestConstraint",
					},
				},
			},
			Targets: []v1beta1.Target{
				{
					Target: "admission.k8s.gatekeeper.sh",
					Rego:   "package test",
					Code: []v1beta1.Code{
						{
							"K8sNativeValidation",
							&templates.Anything{
								Value: source.MustToUnstructured(),
							},
						},
					},
				},
			},
		},
	}

	unversionedCT := &templates.ConstraintTemplate{}
	_ = scheme.Convert(ct, unversionedCT, nil)

	tests := []struct {
		name            string
		ctName          string
		existingObjects []client.Object
		deleteChanged   bool
		connectChanged  bool
		vapOps          []admissionregistrationv1.OperationType
		wantErr         bool
	}{
		{
			name:            "non-existent CT",
			ctName:          "non-existent",
			existingObjects: []client.Object{},
			wantErr:         true,
		},
		{
			name:            "get resource rule ops",
			ctName:          "test-template",
			existingObjects: []client.Object{ct},
			deleteChanged:   false,
			connectChanged:  false,
			vapOps:          []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientBuilder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.existingObjects != nil {
				clientBuilder = clientBuilder.WithObjects(tt.existingObjects...)
			}

			fakeClient := clientBuilder.Build()
			r := &ReconcileVWHC{
				reader: fakeClient,
				writer: fakeClient,
				scheme: scheme,
			}
			// run tests
			_, err := r.getResourceRuleOps(context.Background(), tt.ctName, tt.deleteChanged, tt.connectChanged, tt.vapOps)
			if (err != nil) != tt.wantErr {
				t.Errorf("getResourceRuleOps() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
