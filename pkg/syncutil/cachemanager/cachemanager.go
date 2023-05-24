package cachemanager

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("data-replication").WithValues("metaKind", "CacheManagerTracker")

type CacheManager struct {
	opa              syncutil.OpaDataClient
	syncMetricsCache *syncutil.MetricsCache
	tracker          *readiness.Tracker
	processExcluder  *process.Excluder

	// todo acpana -- integrate gvkaggregator
}

func NewCacheManager(opa syncutil.OpaDataClient, syncMetricsCache *syncutil.MetricsCache, tracker *readiness.Tracker, processExcluder *process.Excluder) *CacheManager {
	return &CacheManager{
		opa:              opa,
		syncMetricsCache: syncMetricsCache,
		tracker:          tracker,
		processExcluder:  processExcluder,
	}
}

func (c *CacheManager) AddObject(ctx context.Context, instance *unstructured.Unstructured) (*types.Responses, error) {
	isNamespaceExcluded, err := c.processExcluder.IsNamespaceExcluded(process.Sync, instance)
	if err != nil {
		return nil, fmt.Errorf("error while excluding namespaces: %w", err)
	}

	// bail because it means we should not be
	// syncing this gvk
	if isNamespaceExcluded {
		// todo acpana -- consider actually calling RemoveGVKToSync in this case
		// as we should not be tracking this GVK anymore
		c.tracker.ForData(instance.GroupVersionKind()).CancelExpect(instance)

		return &types.Responses{}, nil
	}

	syncKey := syncutil.GetKeyForSyncMetrics(instance.GetNamespace(), instance.GetName())
	resp, err := c.opa.AddData(ctx, instance)
	if err != nil {
		c.syncMetricsCache.AddObject(
			syncKey,
			syncutil.Tags{
				Kind:   instance.GetKind(),
				Status: metrics.ErrorStatus,
			},
		)

		return resp, err
	}

	c.tracker.ForData(instance.GroupVersionKind()).Observe(instance)

	c.syncMetricsCache.AddObject(syncKey, syncutil.Tags{
		Kind:   instance.GetKind(),
		Status: metrics.ActiveStatus,
	})
	c.syncMetricsCache.AddKind(instance.GetKind())

	log.V(logging.DebugLevel).Info("[readiness] observed data", "gvk", instance.GroupVersionKind(), "namespace", instance.GetNamespace(), "name", instance.GetName())
	return resp, err
}

func (c *CacheManager) RemoveObject(ctx context.Context, instance *unstructured.Unstructured) (*types.Responses, error) {
	resp, err := c.opa.RemoveData(ctx, instance)
	// only delete from metrics map if the data removal was succcesful
	if err != nil {
		c.syncMetricsCache.DeleteObject(syncutil.GetKeyForSyncMetrics(instance.GetNamespace(), instance.GetName()))

		return resp, err
	}

	c.tracker.ForData(instance.GroupVersionKind()).CancelExpect(instance)
	return resp, err
}

func (c *CacheManager) ReportSyncMetrics(reporter *syncutil.Reporter, log logr.Logger) {
	c.syncMetricsCache.ReportSync(reporter, log)
}
