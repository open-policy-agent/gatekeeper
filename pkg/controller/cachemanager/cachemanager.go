package cachemanager

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type CacheManager struct {
	opa              syncutil.OpaDataClient
	syncMetricsCache *syncutil.MetricsCache
	tracker          *readiness.Tracker
	processExcluder  *process.Excluder
}

func NewCacheManager(opa syncutil.OpaDataClient, syncMetricsCache *syncutil.MetricsCache, tracker *readiness.Tracker, processExcluder *process.Excluder) *CacheManager {
	return &CacheManager{
		opa:              opa,
		syncMetricsCache: syncMetricsCache,
		tracker:          tracker,
		processExcluder:  processExcluder,
	}
}

func (c *CacheManager) AddObject(ctx context.Context, instance *unstructured.Unstructured) error {
	isNamespaceExcluded, err := c.processExcluder.IsNamespaceExcluded(process.Sync, instance)
	if err != nil {
		return fmt.Errorf("error while excluding namespaces: %w", err)
	}

	// bail because it means we should not be
	// syncing this gvk
	if isNamespaceExcluded {
		c.tracker.ForData(instance.GroupVersionKind()).CancelExpect(instance)
		return nil
	}

	syncKey := syncutil.GetKeyForSyncMetrics(instance.GetNamespace(), instance.GetName())
	_, err = c.opa.AddData(ctx, instance)
	if err != nil {
		c.syncMetricsCache.AddObject(
			syncKey,
			syncutil.Tags{
				Kind:   instance.GetKind(),
				Status: metrics.ErrorStatus,
			},
		)

		return err
	}

	c.tracker.ForData(instance.GroupVersionKind()).Observe(instance)

	c.syncMetricsCache.AddObject(syncKey, syncutil.Tags{
		Kind:   instance.GetKind(),
		Status: metrics.ActiveStatus,
	})
	c.syncMetricsCache.AddKind(instance.GetKind())

	return err
}

func (c *CacheManager) RemoveObject(ctx context.Context, instance *unstructured.Unstructured) error {
	if _, err := c.opa.RemoveData(ctx, instance); err != nil {
		return err
	}

	// only delete from metrics map if the data removal was succcesful
	c.syncMetricsCache.DeleteObject(syncutil.GetKeyForSyncMetrics(instance.GetNamespace(), instance.GetName()))
	c.tracker.ForData(instance.GroupVersionKind()).CancelExpect(instance)

	return nil
}

func (c *CacheManager) WipeData(ctx context.Context) error {
	if _, err := c.opa.RemoveData(ctx, target.WipeData()); err != nil {
		return err
	}

	// reset sync cache before sending the metric
	c.syncMetricsCache.ResetCache()
	c.syncMetricsCache.ReportSync()

	return nil
}

func (c *CacheManager) ReportSyncMetrics() {
	c.syncMetricsCache.ReportSync()
}

func (c *CacheManager) ReplaceExcluder(p *process.Excluder) {
	c.processExcluder.Replace(p)
}
