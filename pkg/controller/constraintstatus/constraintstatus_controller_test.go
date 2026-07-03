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

package constraintstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestReconcileSkipsStatusUpdateWhenByPodUnchanged(t *testing.T) {
	constraint, podStatus, desiredByPod := constraintStatusFixture(t)
	setByPod(t, constraint, desiredByPod)

	statusWriter := &recordingStatusWriter{}
	reconciler := newConstraintStatusReconcilerForTest(constraint, []v1beta1.ConstraintPodStatus{podStatus}, statusWriter)

	result, err := reconciler.Reconcile(context.Background(), packedRequestFor(constraint))
	if err != nil {
		t.Fatalf("Reconcile() returned error: %v", err)
	}
	if result != (reconcile.Result{}) {
		t.Fatalf("Reconcile() result = %#v, want empty result", result)
	}
	if statusWriter.updates != 0 {
		t.Fatalf("Status().Update calls = %d, want 0", statusWriter.updates)
	}
}

func TestReconcileRepairsStatusUpdateWhenByPodDiffers(t *testing.T) {
	constraint, podStatus, desiredByPod := constraintStatusFixture(t)
	setByPod(t, constraint, []interface{}{map[string]interface{}{"id": "stale-pod"}})

	statusWriter := &recordingStatusWriter{}
	reconciler := newConstraintStatusReconcilerForTest(constraint, []v1beta1.ConstraintPodStatus{podStatus}, statusWriter)

	result, err := reconciler.Reconcile(context.Background(), packedRequestFor(constraint))
	if err != nil {
		t.Fatalf("Reconcile() returned error: %v", err)
	}
	if result != (reconcile.Result{}) {
		t.Fatalf("Reconcile() result = %#v, want empty result", result)
	}
	if statusWriter.updates != 1 {
		t.Fatalf("Status().Update calls = %d, want 1", statusWriter.updates)
	}
	if statusWriter.last == nil {
		t.Fatal("Status().Update did not record the updated object")
	}
	if !byPodStatusMatches(statusWriter.last.Object, desiredByPod) {
		t.Fatalf("updated byPod = %#v, want %#v", statusWriter.last.Object["status"], desiredByPod)
	}
}

func newConstraintStatusReconcilerForTest(instance *unstructured.Unstructured, statuses []v1beta1.ConstraintPodStatus, statusWriter *recordingStatusWriter) *ReconcileConstraintStatus {
	return &ReconcileConstraintStatus{
		reader:       &constraintStatusReader{instance: instance, statuses: statuses},
		statusClient: &recordingStatusClient{writer: statusWriter},
		log:          logr.Discard(),
		ifWatching: func(_ schema.GroupVersionKind, fn func() error) (bool, error) {
			return true, fn()
		},
	}
}

func constraintStatusFixture(t *testing.T) (*unstructured.Unstructured, v1beta1.ConstraintPodStatus, []interface{}) {
	t.Helper()

	gvk := schema.GroupVersionKind{Group: v1beta1.ConstraintsGroup, Version: "v1beta1", Kind: "K8sRequiredLabels"}
	constraint := &unstructured.Unstructured{}
	constraint.SetGroupVersionKind(gvk)
	constraint.SetName("ns-required")
	constraint.SetUID(types.UID("constraint-uid"))

	podStatus := v1beta1.ConstraintPodStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-a-k8srequiredlabels-ns-required",
			Namespace: util.GetNamespace(),
			Labels: map[string]string{
				v1beta1.ConstraintNameLabel: constraint.GetName(),
				v1beta1.ConstraintKindLabel: constraint.GetKind(),
				v1beta1.PodLabel:            "pod-a",
			},
		},
		Status: v1beta1.ConstraintPodStatusStatus{
			ID:                 "pod-a",
			ConstraintUID:      constraint.GetUID(),
			Operations:         []string{"webhook"},
			Enforced:           true,
			ObservedGeneration: 2,
		},
	}

	return constraint, podStatus, []interface{}{mustStatusMap(t, podStatus.Status)}
}

type constraintStatusReader struct {
	instance *unstructured.Unstructured
	statuses []v1beta1.ConstraintPodStatus
}

func (r *constraintStatusReader) Get(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	if key.Name != r.instance.GetName() || key.Namespace != r.instance.GetNamespace() {
		return apierrors.NewNotFound(schema.GroupResource{Group: r.instance.GroupVersionKind().Group, Resource: r.instance.GetKind()}, key.Name)
	}

	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("unexpected Get object type %T", obj)
	}
	r.instance.DeepCopyInto(u)
	return nil
}

func (r *constraintStatusReader) List(_ context.Context, list client.ObjectList, opts ...client.ListOption) error {
	statusList, ok := list.(*v1beta1.ConstraintPodStatusList)
	if !ok {
		return fmt.Errorf("unexpected List object type %T", list)
	}
	if err := requireIndexedStatusListOptions(opts, map[string]string{
		v1beta1.ConstraintNameLabel: r.instance.GetName(),
		v1beta1.ConstraintKindLabel: r.instance.GetKind(),
	}); err != nil {
		return err
	}
	statusList.Items = append([]v1beta1.ConstraintPodStatus(nil), r.statuses...)
	return nil
}

func requireIndexedStatusListOptions(opts []client.ListOption, fields map[string]string) error {
	listOpts := &client.ListOptions{}
	for _, opt := range opts {
		opt.ApplyToList(listOpts)
	}
	if listOpts.Namespace != util.GetNamespace() {
		return fmt.Errorf("list namespace = %q, want %q", listOpts.Namespace, util.GetNamespace())
	}
	if listOpts.FieldSelector == nil {
		return fmt.Errorf("missing field selector")
	}
	for field, want := range fields {
		got, found := listOpts.FieldSelector.RequiresExactMatch(field)
		if !found || got != want {
			return fmt.Errorf("field selector %q = %q, found %t; want %q", field, got, found, want)
		}
	}
	return nil
}

type recordingStatusClient struct {
	writer *recordingStatusWriter
}

func (c *recordingStatusClient) Status() client.SubResourceWriter {
	return c.writer
}

type recordingStatusWriter struct {
	updates int
	last    *unstructured.Unstructured
}

func (w *recordingStatusWriter) Create(context.Context, client.Object, client.Object, ...client.SubResourceCreateOption) error {
	return nil
}

func (w *recordingStatusWriter) Update(_ context.Context, obj client.Object, _ ...client.SubResourceUpdateOption) error {
	w.updates++
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("unexpected Update object type %T", obj)
	}
	w.last = u.DeepCopy()
	return nil
}

func (w *recordingStatusWriter) Patch(context.Context, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
	return nil
}

func (w *recordingStatusWriter) Apply(context.Context, apimachineryruntime.ApplyConfiguration, ...client.SubResourceApplyOption) error {
	return nil
}

func packedRequestFor(obj client.Object) reconcile.Request {
	return util.EventPackerMapFunc()(context.Background(), obj)[0]
}

func setByPod(t *testing.T, obj *unstructured.Unstructured, byPod []interface{}) {
	t.Helper()
	if err := unstructured.SetNestedSlice(obj.Object, byPod, "status", "byPod"); err != nil {
		t.Fatalf("SetNestedSlice() returned error: %v", err)
	}
}

func mustStatusMap(t *testing.T, status interface{}) map[string]interface{} {
	t.Helper()
	j, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(j, &out); err != nil {
		t.Fatalf("json.Unmarshal() returned error: %v", err)
	}
	return out
}
