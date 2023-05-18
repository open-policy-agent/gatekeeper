package cmt

import (
	"context"
	"sync"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
)

type CacheManagerTracker struct {
	lock sync.RWMutex

	opa              syncutil.OpaDataClient
	SyncMetricsCache *syncutil.MetricsCache
}

func NewCacheManager(opa syncutil.OpaDataClient, syncMetricsCache *syncutil.MetricsCache) *CacheManagerTracker {
	return &CacheManagerTracker{
		opa:              opa,
		SyncMetricsCache: syncMetricsCache,
	}
}

func (c *CacheManagerTracker) AddData(ctx context.Context, data interface{}) (*types.Responses, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.opa.AddData(ctx, data)
}

func (c *CacheManagerTracker) RemoveData(ctx context.Context, data interface{}) (*types.Responses, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	return c.opa.RemoveData(ctx, data)
}
