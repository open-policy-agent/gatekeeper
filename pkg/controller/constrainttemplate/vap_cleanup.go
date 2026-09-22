package constrainttemplate

import (
	"context"
	"errors"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/constraint"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	vapCleanupBatchSize     = 100
	vapCleanupBatchInterval = time.Second
	vapCleanupBatchTimeout  = 30 * time.Second
)

type templateVAPCleanup struct {
	reconciler   *ReconcileConstraintTemplate
	groupVersion schema.GroupVersion
	queue        workqueue.TypedRateLimitingInterface[string]
}

type templateVAPCleanupCandidate struct {
	template *v1beta1.ConstraintTemplate
	policy   client.Object
}

func newTemplateVAPCleanup(reconciler *ReconcileConstraintTemplate, groupVersion schema.GroupVersion) *templateVAPCleanup {
	return &templateVAPCleanup{
		reconciler:   reconciler,
		groupVersion: groupVersion,
		queue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.NewTypedItemExponentialFailureRateLimiter[string](time.Second, time.Minute),
			workqueue.TypedRateLimitingQueueConfig[string]{Name: "template-vap-cleanup"},
		),
	}
}

func (worker *templateVAPCleanup) NeedLeaderElection() bool { return true }

func (worker *templateVAPCleanup) Start(ctx context.Context) error {
	defer worker.queue.ShutDown()
	ticker := time.NewTicker(vapCleanupBatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			worker.processNextBatch(ctx)
		}
	}
}

func (worker *templateVAPCleanup) processNextBatch(ctx context.Context) {
	batchCtx, cancel := context.WithTimeout(ctx, vapCleanupBatchTimeout)
	defer cancel()
	batch := make([]string, 0, vapCleanupBatchSize)
	for len(batch) < vapCleanupBatchSize && worker.queue.Len() > 0 {
		name, shutdown := worker.queue.Get()
		if shutdown {
			break
		}
		batch = append(batch, name)
	}
	candidates := make(map[string]templateVAPCleanupCandidate, len(batch))
	policyNames := make([]string, 0, len(batch))
	for _, name := range batch {
		candidate, err := worker.candidate(batchCtx, name)
		if err != nil || candidate == nil {
			worker.finish(ctx, name, err)
			continue
		}
		candidates[name] = *candidate
		policyNames = append(policyNames, candidate.policy.GetName())
	}
	if len(candidates) == 0 {
		return
	}
	referenced, err := worker.reconciler.vapsReferencedByBindings(batchCtx, &worker.groupVersion, policyNames)
	for name, candidate := range candidates {
		cleanupErr := err
		if cleanupErr == nil && !referenced.Has(candidate.policy.GetName()) {
			cleanupErr = worker.deleteCandidate(batchCtx, candidate)
		}
		worker.finish(ctx, name, cleanupErr)
	}
}

func (worker *templateVAPCleanup) candidate(ctx context.Context, name string) (*templateVAPCleanupCandidate, error) {
	if !operations.IsAssigned(operations.Generate) || constraint.GetVAPGenerationMode() != constraint.VAPGenerationModeConstraint {
		return nil, nil
	}
	if worker.reconciler.apiReader == nil {
		return nil, errors.New("API reader is not configured")
	}
	template := &v1beta1.ConstraintTemplate{}
	if err := worker.reconciler.apiReader.Get(ctx, types.NamespacedName{Name: name}, template); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	policy, err := vapForVersion(&worker.groupVersion)
	if err != nil {
		return nil, err
	}
	if err := worker.reconciler.apiReader.Get(ctx, types.NamespacedName{Name: getVAPName(name)}, policy); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if !template.GetDeletionTimestamp().IsZero() || !policy.GetDeletionTimestamp().IsZero() || !metav1.IsControlledBy(policy, template) {
		return nil, nil
	}
	return &templateVAPCleanupCandidate{template: template, policy: policy}, nil
}

func (worker *templateVAPCleanup) deleteCandidate(ctx context.Context, candidate templateVAPCleanupCandidate) error {
	if !operations.IsAssigned(operations.Generate) || constraint.GetVAPGenerationMode() != constraint.VAPGenerationModeConstraint {
		return nil
	}
	current := &v1beta1.ConstraintTemplate{}
	if err := worker.reconciler.apiReader.Get(ctx, client.ObjectKeyFromObject(candidate.template), current); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !current.GetDeletionTimestamp().IsZero() || current.GetUID() != candidate.template.GetUID() {
		return nil
	}
	return worker.reconciler.deleteVAPIfOwned(ctx, candidate.policy, current)
}

func (worker *templateVAPCleanup) finish(ctx context.Context, name string, err error) {
	defer worker.queue.Done(name)
	if err != nil && ctx.Err() == nil {
		logger.Error(err, "could not clean up shared ValidatingAdmissionPolicy", "template_name", name)
		worker.reconciler.metrics.ReportVAPStatus(types.NamespacedName{Name: name}, metrics.VAPStatusError)
		worker.queue.AddRateLimited(name)
		return
	}
	worker.queue.Forget(name)
	worker.reconciler.metrics.DeleteVAPStatus(types.NamespacedName{Name: name})
}
