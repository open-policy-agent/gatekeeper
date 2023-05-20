package cmt

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type CacheManagerTracker struct {
	lock sync.RWMutex

	opa              syncutil.OpaDataClient
	syncMetricsCache *syncutil.MetricsCache
	tracker          *readiness.Tracker
}

func NewCacheManager(opa syncutil.OpaDataClient, syncMetricsCache *syncutil.MetricsCache) *CacheManagerTracker {
	return &CacheManagerTracker{
		opa:              opa,
		syncMetricsCache: syncMetricsCache,
	}
}

func (c *CacheManagerTracker) WithTracker(newTracker *readiness.Tracker) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.tracker = newTracker
}

func (c *CacheManagerTracker) AddGVKToSync(ctx context.Context, instance *unstructured.Unstructured) (*types.Responses, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

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

	return resp, err
}

func (c *CacheManagerTracker) RemoveGVKFromSync(ctx context.Context, instance *unstructured.Unstructured) (*types.Responses, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	resp, err := c.opa.RemoveData(ctx, instance)
	// only delete from metrics map if the data removal was succcesful
	if err != nil {
		c.syncMetricsCache.DeleteObject(syncutil.GetKeyForSyncMetrics(instance.GetNamespace(), instance.GetName()))

		return resp, err
	}

	c.tracker.ForData(instance.GroupVersionKind()).CancelExpect(instance)
	return resp, err
}

func (c *CacheManagerTracker) ReportSyncMetrics(reporter *syncutil.Reporter, log logr.Logger) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	c.syncMetricsCache.ReportSync(reporter, log)
}
