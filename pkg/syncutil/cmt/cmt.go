package cmt

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type CacheManagerTracker struct {
	lock sync.RWMutex

	opa              syncutil.OpaDataClient
	syncMetricsCache *syncutil.MetricsCache
}

func NewCacheManager(opa syncutil.OpaDataClient, syncMetricsCache *syncutil.MetricsCache) *CacheManagerTracker {
	return &CacheManagerTracker{
		opa:              opa,
		syncMetricsCache: syncMetricsCache,
	}
}

func (c *CacheManagerTracker) AddData(ctx context.Context, instance *unstructured.Unstructured, syncMetricKey *string) (*types.Responses, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	resp, err := c.opa.AddData(ctx, instance)
	if err != nil && syncMetricKey != nil {
		c.AddObjectForSyncMetrics(*syncMetricKey, syncutil.Tags{
			Kind:   instance.GetKind(),
			Status: metrics.ErrorStatus,
		})
	}

	return resp, err
}

// todo call this instance not data.
func (c *CacheManagerTracker) RemoveData(ctx context.Context, instance *unstructured.Unstructured, syncMetricKey *string) (*types.Responses, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	resp, err := c.opa.RemoveData(ctx, instance)
	// only delete from metrics map if the data removal was succcesful
	if err != nil && syncMetricKey != nil {
		c.syncMetricsCache.DeleteObject(*syncMetricKey)
	}

	return resp, err
}

func (c *CacheManagerTracker) ReportSyncMetrics(reporter *syncutil.Reporter, log logr.Logger) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	c.syncMetricsCache.ReportSync(reporter, log)
}

// when/ if readyness tracker becomes part of CMT, then we won't need to have this func.
func (c *CacheManagerTracker) AddObjectForSyncMetrics(syncMetricKey string, tag syncutil.Tags) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.syncMetricsCache.AddObject(syncMetricKey, tag)
}

// when/ if readyness tracker becomes part of CMT, then we won't need to have this func.
func (c *CacheManagerTracker) AddKindForSyncMetrics(syncMetricKind string) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.syncMetricsCache.AddKind(syncMetricKind)
}
