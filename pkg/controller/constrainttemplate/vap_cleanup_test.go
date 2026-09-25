package constrainttemplate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/constraint"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/transform"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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

func TestTemplateVAPCleanupRegisteredWithoutDiscovery(t *testing.T) {
	setConstraintVAPGenerationMode(t)
	setVAPTestGlobals(t, &admissionregistrationv1.SchemeGroupVersion)
	transform.SetVapAPIEnabled(ptr.To(false))
	manager, watchManager := testutils.SetupManager(t, cfg)
	events := make(chan event.GenericEvent, 1024)
	reconciler, err := newReconciler(manager, nil, watchManager, nil, events, events, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, reconciler.vapCleanup, "cleanup must be registered independently of startup discovery")
	t.Cleanup(reconciler.vapCleanup.queue.ShutDown)
}

type observedCleanupQueue struct {
	workqueue.TypedRateLimitingInterface[string]
	events atomic.Int64
}

func (queue *observedCleanupQueue) Add(name string) {
	queue.events.Add(1)
	queue.TypedRateLimitingInterface.Add(name)
}

func TestTemplateVAPCleanupStartupAndBindingEvents(t *testing.T) {
	for _, failDiscovery := range []bool{false, true} {
		t.Run(fmt.Sprintf("discoveryFailure=%t", failDiscovery), func(t *testing.T) {
			setConstraintVAPGenerationMode(t)
			setVAPTestGlobals(t, &admissionregistrationv1.SchemeGroupVersion)
			backend, err := url.Parse(cfg.Host)
			require.NoError(t, err)
			proxy := httputil.NewSingleHostReverseProxy(backend)
			proxy.Transport, err = rest.TransportFor(cfg)
			require.NoError(t, err)
			var available atomic.Bool
			var failures atomic.Int64
			available.Store(!failDiscovery)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if !available.Load() && (request.URL.Path == "/apis/admissionregistration.k8s.io/v1" || request.URL.Path == "/apis/admissionregistration.k8s.io/v1beta1") {
					failures.Add(1)
					http.Error(writer, "discovery temporarily unavailable", http.StatusServiceUnavailable)
					return
				}
				proxy.ServeHTTP(writer, request)
			}))
			t.Cleanup(server.Close)
			kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
			require.NoError(t, clientcmd.WriteToFile(clientcmdapi.Config{
				Clusters:       map[string]*clientcmdapi.Cluster{"test": {Server: server.URL}},
				Contexts:       map[string]*clientcmdapi.Context{"test": {Cluster: "test"}},
				CurrentContext: "test",
			}, kubeconfig))
			testutils.Setenv(t, "KUBECONFIG", kubeconfig)
			transform.SetVapAPIEnabled(nil)
			transform.SetGroupVersion(nil)
			manager, _ := testutils.SetupManager(t, cfg)
			constraintEvents := make(chan event.GenericEvent, 1024)
			reconciler := &ReconcileConstraintTemplate{Client: manager.GetClient(), apiReader: manager.GetAPIReader(), metrics: newStatsReporter(), cstrEvents: constraintEvents}
			worker := newTemplateVAPCleanup(reconciler, admissionregistrationv1.SchemeGroupVersion)
			worker.cache = manager.GetCache()
			queue := &observedCleanupQueue{TypedRateLimitingInterface: worker.queue}
			worker.queue = queue
			require.NoError(t, manager.Add(worker))
			var templateReconciles atomic.Int64
			require.NoError(t, add(manager, reconcile.Func(func(_ context.Context, request reconcile.Request) (reconcile.Result, error) {
				if request.Name == "testkind" {
					templateReconciles.Add(1)
				}
				return reconcile.Result{}, nil
			}), nil))
			ctx := context.Background()
			testutils.StartManager(ctx, t, manager)
			if failDiscovery {
				require.Eventually(t, func() bool { return failures.Load() >= 4 }, 10*time.Second, 20*time.Millisecond)
				available.Store(true)
			}
			for index := 0; index < 10; index++ {
				name := fmt.Sprintf("cleanup-event-%t-%d", failDiscovery, index)
				binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
					TypeMeta: metav1.TypeMeta{APIVersion: admissionregistrationv1.SchemeGroupVersion.String(), Kind: "ValidatingAdmissionPolicyBinding"},
					ObjectMeta: metav1.ObjectMeta{Name: name, OwnerReferences: []metav1.OwnerReference{{
						APIVersion: "constraints.gatekeeper.sh/v1beta1", Kind: "TestKind", Name: name, UID: types.UID(name), Controller: ptr.To(true),
					}}},
					Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
						PolicyName: "gatekeeper-testkind", ValidationActions: []admissionregistrationv1.ValidationAction{admissionregistrationv1.Audit},
					},
				}
				testutils.CreateThenCleanup(ctx, t, manager.GetClient(), binding)
				require.Eventually(t, func() bool { return queue.events.Load() >= int64(index*2+1) }, 10*time.Second, 20*time.Millisecond)
				require.NoError(t, manager.GetClient().Get(ctx, client.ObjectKeyFromObject(binding), binding))
				binding.Spec.PolicyName = "gatekeeper-testkind-" + name + "-vap"
				require.NoError(t, manager.GetClient().Update(ctx, binding))
				require.Eventually(t, func() bool { return queue.events.Load() >= int64(index*2+2) }, 10*time.Second, 20*time.Millisecond)
			}
			require.Zero(t, templateReconciles.Load(), "binding events must not enter template reconciliation or trigger constraint-wide fanout")
			require.Empty(t, constraintEvents)
			bindings := &admissionregistrationv1.ValidatingAdmissionPolicyBindingList{}
			require.NoError(t, manager.GetClient().List(ctx, bindings, client.MatchingFields{vapBindingPolicyNameField: "gatekeeper-testkind"}))
			require.Empty(t, bindings.Items, "the live binding index must track migrations after discovery recovers")
		})
	}
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

type delayedCleanupReader struct {
	client.Reader
	delay time.Duration
}

func (reader *delayedCleanupReader) Get(ctx context.Context, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
	timer := time.NewTimer(reader.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return reader.Reader.Get(ctx, key, object, options...)
	}
}

func TestTemplateVAPCleanupProgressWithSlowReads(t *testing.T) {
	worker, live, policies := newCleanupTestWorker(t, admissionregistrationv1.SchemeGroupVersion, 200)
	worker.reconciler.Client = &cleanupWriteClient{Client: worker.reconciler.Client, writer: live}
	worker.reconciler.apiReader = &delayedCleanupReader{Reader: live, delay: 10 * time.Millisecond}
	for index := range policies {
		worker.queue.Add(fmt.Sprintf("template-%03d", index))
	}
	remaining := len(policies)
	for batch := 0; remaining > 0 && batch < 50; batch++ {
		ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
		worker.processNextBatch(ctx)
		cancel()
		current := 0
		for _, policy := range policies {
			err := live.Get(context.Background(), client.ObjectKeyFromObject(policy), policy)
			if !apierrors.IsNotFound(err) {
				require.NoError(t, err)
				current++
			}
		}
		require.Less(t, current, remaining, "each batch must make progress under sustained read latency")
		remaining = current
	}
	require.Zero(t, remaining)
	require.Zero(t, worker.queue.Len())
}

func TestTemplateVAPCleanupSlowSingleCandidate(t *testing.T) {
	worker, live, policies := newCleanupTestWorker(t, admissionregistrationv1.SchemeGroupVersion, 1)
	worker.reconciler.Client = &cleanupWriteClient{Client: worker.reconciler.Client, writer: live}
	worker.reconciler.apiReader = &delayedCleanupReader{Reader: live, delay: 60 * time.Millisecond}
	worker.queue.Add("template-000")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	worker.processNextBatch(ctx)
	require.True(t, apierrors.IsNotFound(live.Get(context.Background(), client.ObjectKeyFromObject(policies[0]), policies[0])))
	require.Zero(t, worker.queue.Len())
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
