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

package constrainttemplatestatus

import (
	"context"
	"testing"

	templatesv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const unitTestTemplateName = "unit-test-template"

func newStatusUnitReconciler(t *testing.T, ct *templatesv1beta1.ConstraintTemplate, podStatuses ...*statusv1beta1.ConstraintTemplatePodStatus) *ReconcileConstraintStatus {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := templatesv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := statusv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	objs := []runtime.Object{ct}
	for _, s := range podStatuses {
		objs = append(objs, s)
	}
	fakeClient := clientfake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&templatesv1beta1.ConstraintTemplate{}).
		WithRuntimeObjects(objs...).
		Build()

	return &ReconcileConstraintStatus{
		reader:       fakeClient,
		writer:       fakeClient,
		statusClient: fakeClient,
		scheme:       scheme,
		log:          log,
	}
}

// TestReconcile_CreatedIsMonotonic verifies that status.created never regresses from true to
// false when every current pod status reports errors: it stays true if it was already true
// (the CRD backing the ConstraintTemplate is never garbage collected unless the
// ConstraintTemplate itself is deleted), and it stays false if it was never true, so a
// genuine failure is not masked.
func TestReconcile_CreatedIsMonotonic(t *testing.T) {
	tests := []struct {
		name         string
		priorCreated bool
		wantCreated  bool
	}{
		{
			name:         "previously created stays true when all pod statuses have errors",
			priorCreated: true,
			wantCreated:  true,
		},
		{
			name:         "never created stays false when all pod statuses have errors",
			priorCreated: false,
			wantCreated:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uid := types.UID("test-uid")
			ct := &templatesv1beta1.ConstraintTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: unitTestTemplateName,
					UID:  uid,
				},
				Status: templatesv1beta1.ConstraintTemplateStatus{
					Created: tt.priorCreated,
				},
			}

			erroredStatus := newErroredPodStatus(t, "pod1", uid)

			r := newStatusUnitReconciler(t, ct, erroredStatus)

			if _, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: unitTestTemplateName}}); err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}

			got := &templatesv1beta1.ConstraintTemplate{}
			if err := r.reader.Get(context.Background(), types.NamespacedName{Name: unitTestTemplateName}, got); err != nil {
				t.Fatal(err)
			}
			if got.Status.Created != tt.wantCreated {
				t.Errorf("status.created = %t, want %t", got.Status.Created, tt.wantCreated)
			}
		})
	}
}

func newErroredPodStatus(t *testing.T, podName string, templateUID types.UID) *statusv1beta1.ConstraintTemplatePodStatus {
	t.Helper()

	name, err := statusv1beta1.KeyForConstraintTemplate(podName, unitTestTemplateName)
	if err != nil {
		t.Fatal(err)
	}
	return &statusv1beta1.ConstraintTemplatePodStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: util.GetNamespace(),
			Labels: map[string]string{
				statusv1beta1.ConstraintTemplateNameLabel: unitTestTemplateName,
				statusv1beta1.PodLabel:                    podName,
			},
		},
		Status: statusv1beta1.ConstraintTemplatePodStatusStatus{
			ID:          podName,
			TemplateUID: templateUID,
			Errors: []*templatesv1beta1.CreateCRDError{{
				Code:    "create_error",
				Message: "could not create CRD",
			}},
		},
	}
}
