package constraint

import (
	"context"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/transform"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type constraintVAPWatches struct {
	controller     controller.Controller
	cache          cache.Cache
	generationMode VAPGenerationMode
	bindingStarted bool
	policyStarted  bool
}

func (watches *constraintVAPWatches) NeedLeaderElection() bool { return true }

func (watches *constraintVAPWatches) Start(ctx context.Context) error {
	retry := time.NewTicker(time.Second)
	defer retry.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		complete, err := watches.start(ctx)
		if err != nil {
			log.Error(err, "could not initialize Constraint policy watches, will retry")
		}
		if complete {
			<-ctx.Done()
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-retry.C:
		}
	}
}

func (watches *constraintVAPWatches) start(ctx context.Context) (bool, error) {
	enabled, groupVersion := transform.IsVapAPIEnabled(&log)
	if !enabled || groupVersion == nil {
		return false, nil
	}
	if !watches.bindingStarted {
		binding, err := vapBindingForVersion(*groupVersion)
		if err != nil {
			return false, err
		}
		if err := watches.watch(ctx, binding); err != nil {
			return false, err
		}
		watches.bindingStarted = true
	}
	if watches.generationMode == VAPGenerationModeConstraint && !watches.policyStarted {
		policy, err := vapForVersion(groupVersion)
		if err != nil {
			return false, err
		}
		if err := watches.watch(ctx, policy); err != nil {
			return false, err
		}
		watches.policyStarted = true
	}
	return true, nil
}

func (watches *constraintVAPWatches) watch(ctx context.Context, object client.Object) error {
	syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := watches.cache.GetInformer(syncCtx, object); err != nil {
		return err
	}
	return watches.controller.Watch(source.Kind(watches.cache, object, handler.TypedEnqueueRequestsFromMapFunc(eventPackerMapFuncFromOwnerRefs())))
}
