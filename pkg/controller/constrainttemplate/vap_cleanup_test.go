package constrainttemplate

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/constraint"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newCleanupTestWorker(t *testing.T, groupVersion schema.GroupVersion, count int) (*templateVAPCleanup, *vapBindingListClient, []client.Object) {
	t.Helper()
	setConstraintVAPGenerationMode(t)
	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))
	require.NoError(t, admissionregistrationv1.AddToScheme(scheme))
	require.NoError(t, admissionregistrationv1beta1.AddToScheme(scheme))
	objects := make([]client.Object, 0, count*2)
	policies := make([]client.Object, 0, count)
	for index := 0; index < count; index++ {
		name := fmt.Sprintf("template-%03d", index)
		template := &v1beta1.ConstraintTemplate{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name)}}
		policy, err := vapForVersion(&groupVersion)
		require.NoError(t, err)
		policy.SetName(getVAPName(name))
		policy.SetUID(types.UID("policy-" + name))
		policy.SetResourceVersion("1")
		policy.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion: v1beta1.SchemeGroupVersion.String(), Kind: "ConstraintTemplate", Name: name,
			UID: template.GetUID(), Controller: ptr.To(true),
		}})
		objects = append(objects, template, policy)
		policies = append(policies, policy)
	}
	prototype, err := vapBindingForVersion(&groupVersion)
	require.NoError(t, err)
	cached := crfake.NewClientBuilder().WithScheme(scheme).WithIndex(prototype, vapBindingPolicyNameField, vapBindingPolicyNames).Build()
	live := &vapBindingListClient{Client: crfake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}
	reconciler := &ReconcileConstraintTemplate{Client: cached, apiReader: live, metrics: newStatsReporter()}
	worker := newTemplateVAPCleanup(reconciler, groupVersion)
	reconciler.vapCleanup = worker
	t.Cleanup(worker.queue.ShutDown)
	return worker, live, policies
}

func addCleanupTestBinding(t *testing.T, writer client.Writer, groupVersion schema.GroupVersion, policy string) {
	t.Helper()
	binding, err := vapBindingForVersion(&groupVersion)
	require.NoError(t, err)
	binding.SetName(policy + "-binding")
	switch typed := binding.(type) {
	case *admissionregistrationv1.ValidatingAdmissionPolicyBinding:
		typed.Spec.PolicyName = policy
	case *admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding:
		typed.Spec.PolicyName = policy
	}
	require.NoError(t, writer.Create(context.Background(), binding))
}

type cleanupWriteClient struct {
	client.Client
	writer client.Writer
}

func (writer *cleanupWriteClient) Delete(ctx context.Context, object client.Object, options ...client.DeleteOption) error {
	return writer.writer.Delete(ctx, object, options...)
}

func TestTemplateVAPCleanupBatchesAndRetainsReferences(t *testing.T) {
	for _, groupVersion := range []schema.GroupVersion{admissionregistrationv1.SchemeGroupVersion, admissionregistrationv1beta1.SchemeGroupVersion} {
		t.Run(groupVersion.Version, func(t *testing.T) {
			worker, live, policies := newCleanupTestWorker(t, groupVersion, 3)
			worker.reconciler.Client = &cleanupWriteClient{Client: worker.reconciler.Client, writer: live}
			addCleanupTestBinding(t, worker.reconciler.Client, groupVersion, policies[0].GetName())
			addCleanupTestBinding(t, live, groupVersion, policies[1].GetName())
			for index := range policies {
				name := fmt.Sprintf("template-%03d", index)
				worker.queue.Add(name)
				worker.queue.Add(name)
			}
			require.Equal(t, 3, worker.queue.Len())
			worker.processNextBatch(context.Background())
			require.Equal(t, 1, live.listCalls)
			require.Zero(t, worker.queue.Len())
			for index, policy := range policies {
				err := live.Get(context.Background(), client.ObjectKeyFromObject(policy), policy)
				if index == 2 {
					require.True(t, apierrors.IsNotFound(err))
				} else {
					require.NoError(t, err, "both cached and live references must retain their policies")
				}
			}
		})
	}
}

func TestTemplateVAPCleanupFailureRetriesWholeSnapshot(t *testing.T) {
	for _, groupVersion := range []schema.GroupVersion{admissionregistrationv1.SchemeGroupVersion, admissionregistrationv1beta1.SchemeGroupVersion} {
		t.Run(groupVersion.Version, func(t *testing.T) {
			worker, live, policies := newCleanupTestWorker(t, groupVersion, 2)
			worker.reconciler.Client = &cleanupWriteClient{Client: worker.reconciler.Client, writer: live}
			live.listPage = func(list client.ObjectList, options client.ListOptions) error {
				if options.Continue != "" {
					return errors.New("second page unavailable")
				}
				list.SetContinue("second-page")
				return nil
			}
			for index := range policies {
				worker.queue.Add(fmt.Sprintf("template-%03d", index))
			}
			worker.processNextBatch(context.Background())
			require.Equal(t, 2, live.listCalls)
			require.EqualValues(t, 2, worker.reconciler.metrics.vapRegistry.ComputeTotals()[metrics.VAPStatusError])
			for index, policy := range policies {
				require.NoError(t, live.Get(context.Background(), client.ObjectKeyFromObject(policy), policy))
				name := fmt.Sprintf("template-%03d", index)
				require.Equal(t, 1, worker.queue.NumRequeues(name))
				worker.queue.Add(name)
			}
			live.listPage = nil
			addCleanupTestBinding(t, live, groupVersion, policies[0].GetName())
			worker.processNextBatch(context.Background())
			require.Equal(t, 3, live.listCalls, "retry must perform a fresh scan")
			require.NoError(t, live.Get(context.Background(), client.ObjectKeyFromObject(policies[0]), policies[0]))
			require.True(t, apierrors.IsNotFound(live.Get(context.Background(), client.ObjectKeyFromObject(policies[1]), policies[1])))
			require.Zero(t, worker.reconciler.metrics.vapRegistry.ComputeTotals()[metrics.VAPStatusError])
		})
	}
}

func TestTemplateVAPCleanupBoundedSnapshot(t *testing.T) {
	worker, live, policies := newCleanupTestWorker(t, admissionregistrationv1.SchemeGroupVersion, vapCleanupBatchSize+1)
	worker.reconciler.Client = &cleanupWriteClient{Client: worker.reconciler.Client, writer: live}
	for index := 0; index < vapCleanupBatchSize; index++ {
		worker.queue.Add(fmt.Sprintf("template-%03d", index))
	}
	lateName := fmt.Sprintf("template-%03d", vapCleanupBatchSize)
	live.listPage = func(list client.ObjectList, options client.ListOptions) error {
		worker.queue.Add(lateName)
		return live.Client.List(context.Background(), list, &options)
	}
	worker.processNextBatch(context.Background())
	require.Equal(t, 1, live.listCalls)
	require.Equal(t, 1, worker.queue.Len())
	latePolicy := policies[vapCleanupBatchSize]
	require.NoError(t, live.Get(context.Background(), client.ObjectKeyFromObject(latePolicy), latePolicy))
	live.listPage = nil
	addCleanupTestBinding(t, live, admissionregistrationv1.SchemeGroupVersion, latePolicy.GetName())
	worker.processNextBatch(context.Background())
	require.Equal(t, 2, live.listCalls, "late candidate cannot use an older negative snapshot")
	require.NoError(t, live.Get(context.Background(), client.ObjectKeyFromObject(latePolicy), latePolicy))
	require.Zero(t, worker.queue.Len())
}

func TestTemplateVAPCleanupLimitsBatchSize(t *testing.T) {
	worker, live, policies := newCleanupTestWorker(t, admissionregistrationv1.SchemeGroupVersion, vapCleanupBatchSize+1)
	worker.reconciler.Client = &cleanupWriteClient{Client: worker.reconciler.Client, writer: live}
	for index := range policies {
		worker.queue.Add(fmt.Sprintf("template-%03d", index))
	}
	worker.processNextBatch(context.Background())
	require.Equal(t, 1, live.listCalls)
	require.Equal(t, 1, worker.queue.Len(), "overflow must remain queued for another batch")
	for index, policy := range policies {
		err := live.Get(context.Background(), client.ObjectKeyFromObject(policy), policy)
		if index == vapCleanupBatchSize {
			require.NoError(t, err)
		} else {
			require.True(t, apierrors.IsNotFound(err))
		}
	}
	worker.processNextBatch(context.Background())
	require.Equal(t, 2, live.listCalls)
	require.Zero(t, worker.queue.Len())
	require.True(t, apierrors.IsNotFound(live.Get(context.Background(), client.ObjectKeyFromObject(policies[vapCleanupBatchSize]), policies[vapCleanupBatchSize])))
}

func TestTemplateVAPCleanupRevalidatesBeforeDelete(t *testing.T) {
	for _, scenario := range []string{"template recreated", "policy changed", "mode changed"} {
		t.Run(scenario, func(t *testing.T) {
			worker, live, policies := newCleanupTestWorker(t, admissionregistrationv1.SchemeGroupVersion, 1)
			worker.reconciler.Client = &cleanupWriteClient{Client: worker.reconciler.Client, writer: live}
			live.listPage = func(list client.ObjectList, options client.ListOptions) error {
				switch scenario {
				case "template recreated":
					template := &v1beta1.ConstraintTemplate{}
					require.NoError(t, live.Get(context.Background(), types.NamespacedName{Name: "template-000"}, template))
					require.NoError(t, live.Delete(context.Background(), template))
					template.SetResourceVersion("")
					template.SetUID("new-template")
					require.NoError(t, live.Create(context.Background(), template))
				case "policy changed":
					policy := &admissionregistrationv1.ValidatingAdmissionPolicy{}
					require.NoError(t, live.Get(context.Background(), client.ObjectKeyFromObject(policies[0]), policy))
					policy.SetAnnotations(map[string]string{"changed": "during-scan"})
					require.NoError(t, live.Update(context.Background(), policy))
				case "mode changed":
					require.NoError(t, constraint.SetVAPGenerationMode(constraint.VAPGenerationModeTemplate))
				}
				return live.Client.List(context.Background(), list, &options)
			}
			worker.queue.Add("template-000")
			worker.processNextBatch(context.Background())
			require.NoError(t, live.Get(context.Background(), client.ObjectKeyFromObject(policies[0]), policies[0]))
			if scenario == "policy changed" {
				require.Equal(t, 1, worker.queue.NumRequeues("template-000"), "conflict must retry with a new snapshot")
			}
		})
	}
}

func TestTemplateVAPCleanupCancellation(t *testing.T) {
	worker, live, policies := newCleanupTestWorker(t, admissionregistrationv1.SchemeGroupVersion, 1)
	worker.reconciler.Client = &cleanupWriteClient{Client: worker.reconciler.Client, writer: live}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	live.listPage = func(client.ObjectList, client.ListOptions) error {
		cancel()
		return ctx.Err()
	}
	worker.queue.Add("template-000")
	worker.processNextBatch(ctx)
	require.NoError(t, live.Get(context.Background(), client.ObjectKeyFromObject(policies[0]), policies[0]))
	require.Zero(t, worker.queue.NumRequeues("template-000"))
	done := make(chan error, 1)
	go func() { done <- worker.Start(ctx) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("cleanup worker did not stop after cancellation")
	}
	require.True(t, worker.queue.ShuttingDown())
}
