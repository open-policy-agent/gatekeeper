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

package expansionstatus

import (
	"context"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/apis"
	expansionv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/expansion/v1beta1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestReconcileSkipsSecondIdenticalStatusUpdate(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)

	template := &expansionv1beta1.ExpansionTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "expansion-template-a",
			UID:  types.UID("expansion-template-uid"),
		},
	}
	podStatus := &statusv1beta1.ExpansionTemplatePodStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-a-expansion-template",
			Namespace: util.GetNamespace(),
			Labels: map[string]string{
				statusv1beta1.ExpansionTemplateNameLabel: template.Name,
			},
		},
		Status: statusv1beta1.ExpansionTemplatePodStatusStatus{
			ID:          "pod-a",
			TemplateUID: template.UID,
		},
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithIndex(&statusv1beta1.ExpansionTemplatePodStatus{}, statusv1beta1.ExpansionTemplateNameLabel, indexObjectByLabel(statusv1beta1.ExpansionTemplateNameLabel)).WithObjects(template, podStatus).Build()
	statusClient := &recordingStatusClient{client: k8sClient}
	reconciler := &ReconcileExpansionStatus{
		reader:       k8sClient,
		statusClient: statusClient,
	}
	request := reconcile.Request{NamespacedName: types.NamespacedName{Name: template.Name}}

	_, err := reconciler.Reconcile(ctx, request)
	require.NoError(t, err)
	require.Equal(t, 1, statusClient.updateCount, "first reconcile should self-heal missing aggregate status")

	updated := &expansionv1beta1.ExpansionTemplate{}
	require.NoError(t, k8sClient.Get(ctx, request.NamespacedName, updated))
	require.Len(t, updated.Status.ByPod, 1)

	_, err = reconciler.Reconcile(ctx, request)
	require.NoError(t, err)
	require.Equal(t, 1, statusClient.updateCount, "second reconcile with identical byPod status should not write")
}

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, apis.AddToScheme(scheme))
	return scheme
}

func indexObjectByLabel(label string) client.IndexerFunc {
	return func(obj client.Object) []string {
		value := obj.GetLabels()[label]
		if value == "" {
			return nil
		}
		return []string{value}
	}
}

type recordingStatusClient struct {
	client      client.Client
	updateCount int
}

func (c *recordingStatusClient) Status() client.SubResourceWriter {
	return recordingStatusWriter{client: c.client, updateCount: &c.updateCount}
}

type recordingStatusWriter struct {
	client      client.Client
	updateCount *int
}

func (w recordingStatusWriter) Create(context.Context, client.Object, client.Object, ...client.SubResourceCreateOption) error {
	panic("unexpected status create")
}

func (w recordingStatusWriter) Update(ctx context.Context, obj client.Object, _ ...client.SubResourceUpdateOption) error {
	(*w.updateCount)++
	return w.client.Update(ctx, obj)
}

func (w recordingStatusWriter) Patch(context.Context, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
	panic("unexpected status patch")
}

func (w recordingStatusWriter) Apply(context.Context, runtime.ApplyConfiguration, ...client.SubResourceApplyOption) error {
	panic("unexpected status apply")
}
