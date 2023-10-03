package expectationsmgr

import (
	"context"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
)

const tickDuration = 3 * time.Second

// ExpectationsMgr fires after data expectations have been populated in the ready Tracker and runs
// until the Tracker is satisfeid or the process is exiting. It removes Data expectations for any
// GVKs that are expected in the Tracker but not watched by the CacheManager.
type ExpectationsMgr struct {
	cacheMgr *cachemanager.CacheManager
	tracker  *readiness.Tracker
}

func NewExpecationsManager(cm *cachemanager.CacheManager, rt *readiness.Tracker) *ExpectationsMgr {
	return &ExpectationsMgr{
		cacheMgr: cm,
		tracker:  rt,
	}
}

func (e *ExpectationsMgr) Run(ctx context.Context) {
	ticker := time.NewTicker(tickDuration)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if e.tracker.Satisfied() {
				// we're done, there's no need to
				// further manage the data sync expectations.
				return
			}
			if !(e.tracker.DataPopulated() && e.cacheMgr.Started()) {
				// we have to wait on data expectations to be populated
				// and for the cachemanager to have been started by the
				// controller manager.
				break
			}

			watchedGVKs := watch.NewSet()
			watchedGVKs.Add(e.cacheMgr.WatchedGVKs()...)
			expectedGVKs := watch.NewSet()
			expectedGVKs.Add(e.tracker.DataGVKs()...)

			for _, gvk := range expectedGVKs.Difference(watchedGVKs).Items() {
				e.tracker.CancelData(gvk)
			}
		}
	}
}
