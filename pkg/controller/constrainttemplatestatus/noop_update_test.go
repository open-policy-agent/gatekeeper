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
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	constrainttemplatev1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestReconcileSkipsSecondIdenticalStatusUpdate(t *testing.T) {
	ctx := context.Background()

	template := &unstructured.Unstructured{}
	template.SetGroupVersionKind(constrainttemplatev1beta1.SchemeGroupVersion.WithKind("ConstraintTemplate"))
	template.SetName("template-a")
	template.SetUID(types.UID("template-uid"))

	podStatus := statusv1beta1.ConstraintTemplatePodStatus{
		Status: statusv1beta1.ConstraintTemplatePodStatusStatus{
			ID:          "pod-a",
			TemplateUID: template.GetUID(),
		},
	}

	k8sClient := &memoryClient{
		template:    template.DeepCopy(),
		podStatuses: []statusv1beta1.ConstraintTemplatePodStatus{podStatus},
	}
	statusClient := &recordingStatusClient{client: k8sClient}
	reconciler := &ReconcileConstraintStatus{
		reader:       k8sClient,
		statusClient: statusClient,
		log:          logr.Discard(),
	}
	request := reconcile.Request{NamespacedName: types.NamespacedName{Name: template.GetName()}}

	_, err := reconciler.Reconcile(ctx, request)
	require.NoError(t, err)
	require.Equal(t, 1, statusClient.updateCount, "first reconcile should self-heal missing aggregate status")

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(constrainttemplatev1beta1.SchemeGroupVersion.WithKind("ConstraintTemplate"))
	require.NoError(t, k8sClient.Get(ctx, request.NamespacedName, updated))
	byPod, _, err := unstructured.NestedSlice(updated.Object, "status", "byPod")
	require.NoError(t, err)
	require.Len(t, byPod, 1)
	created, _, err := unstructured.NestedBool(updated.Object, "status", "created")
	require.NoError(t, err)
	require.True(t, created)

	_, err = reconciler.Reconcile(ctx, request)
	require.NoError(t, err)
	require.Equal(t, 1, statusClient.updateCount, "second reconcile with identical byPod and created status should not write")
}

func TestReconcileRepairsMalformedByPodStatus(t *testing.T) {
	ctx := context.Background()

	template := &unstructured.Unstructured{}
	template.SetGroupVersionKind(constrainttemplatev1beta1.SchemeGroupVersion.WithKind("ConstraintTemplate"))
	template.SetName("template-a")
	template.SetUID(types.UID("template-uid"))
	require.NoError(t, unstructured.SetNestedField(template.Object, "malformed", "status", "byPod"))

	podStatus := statusv1beta1.ConstraintTemplatePodStatus{
		Status: statusv1beta1.ConstraintTemplatePodStatusStatus{
			ID:          "pod-a",
			TemplateUID: template.GetUID(),
		},
	}

	k8sClient := &memoryClient{
		template:    template.DeepCopy(),
		podStatuses: []statusv1beta1.ConstraintTemplatePodStatus{podStatus},
	}
	statusClient := &recordingStatusClient{client: k8sClient}
	reconciler := &ReconcileConstraintStatus{
		reader:       k8sClient,
		statusClient: statusClient,
		log:          logr.Discard(),
	}
	request := reconcile.Request{NamespacedName: types.NamespacedName{Name: template.GetName()}}

	_, err := reconciler.Reconcile(ctx, request)
	require.NoError(t, err)
	require.Equal(t, 1, statusClient.updateCount, "malformed byPod should be repaired")

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(constrainttemplatev1beta1.SchemeGroupVersion.WithKind("ConstraintTemplate"))
	require.NoError(t, k8sClient.Get(ctx, request.NamespacedName, updated))
	byPod, _, err := unstructured.NestedSlice(updated.Object, "status", "byPod")
	require.NoError(t, err)
	require.Len(t, byPod, 1)
	created, _, err := unstructured.NestedBool(updated.Object, "status", "created")
	require.NoError(t, err)
	require.True(t, created)
}

type memoryClient struct {
	client.Client
	template    *unstructured.Unstructured
	podStatuses []statusv1beta1.ConstraintTemplatePodStatus
}

func (c *memoryClient) Get(_ context.Context, _ types.NamespacedName, obj client.Object, _ ...client.GetOption) error {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("unexpected Get object type %T", obj)
	}
	*u = *c.template.DeepCopy()
	return nil
}

func (c *memoryClient) List(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
	statuses, ok := list.(*statusv1beta1.ConstraintTemplatePodStatusList)
	if !ok {
		return fmt.Errorf("unexpected List object type %T", list)
	}
	statuses.Items = append([]statusv1beta1.ConstraintTemplatePodStatus(nil), c.podStatuses...)
	return nil
}

type recordingStatusClient struct {
	client      *memoryClient
	updateCount int
}

func (c *recordingStatusClient) Status() client.SubResourceWriter {
	return recordingStatusWriter{client: c.client, updateCount: &c.updateCount}
}

type recordingStatusWriter struct {
	client      *memoryClient
	updateCount *int
}

func (w recordingStatusWriter) Create(context.Context, client.Object, client.Object, ...client.SubResourceCreateOption) error {
	panic("unexpected status create")
}

func (w recordingStatusWriter) Update(_ context.Context, obj client.Object, _ ...client.SubResourceUpdateOption) error {
	(*w.updateCount)++
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("unexpected Update object type %T", obj)
	}
	w.client.template = u.DeepCopy()
	return nil
}

func (w recordingStatusWriter) Patch(context.Context, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
	panic("unexpected status patch")
}

func (w recordingStatusWriter) Apply(context.Context, runtime.ApplyConfiguration, ...client.SubResourceApplyOption) error {
	panic("unexpected status apply")
}
