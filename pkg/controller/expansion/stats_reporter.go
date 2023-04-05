package expansion

import (
	"context"

	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"k8s.io/apimachinery/pkg/types"
)

const (
	etMetricName = "expansion_templates"
	etDesc       = "Number of observed expansion templates"
)

var (
	etM       = stats.Int64(etMetricName, etDesc, stats.UnitDimensionless)
	statusKey = tag.MustNewKey("status")

	views = []*view.View{
		{
			Name:        etMetricName,
			Measure:     etM,
			Description: etDesc,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{statusKey},
		},
	}
)

func init() {
	if err := register(); err != nil {
		panic(err)
	}
}

func register() error {
	return view.Register(views...)
}

func newRegistry() *etRegistry {
	return &etRegistry{cache: make(map[types.NamespacedName]metrics.Status)}
}

type etRegistry struct {
	cache map[types.NamespacedName]metrics.Status
	dirty bool
}

func (r *etRegistry) add(key types.NamespacedName, status metrics.Status) {
	v, ok := r.cache[key]
	if ok && v == status {
		return
	}
	r.cache[key] = status
	r.dirty = true
}

func (r *etRegistry) remove(key types.NamespacedName) {
	if _, exists := r.cache[key]; !exists {
		return
	}
	delete(r.cache, key)
	r.dirty = true
}

func (r *etRegistry) report(ctx context.Context) {
	if !r.dirty {
		return
	}

	totals := make(map[metrics.Status]int64)
	for _, status := range r.cache {
		totals[status]++
	}

	for _, s := range metrics.AllStatuses {
		ctx, err := tag.New(ctx, tag.Insert(statusKey, string(s)))
		if err != nil {
			log.Error(err, "failed to create status tag for expansion templates")
			continue
		}
		err = metrics.Record(ctx, etM.M(totals[s]))
		if err != nil {
			log.Error(err, "failed to record total expansion templates")
		}
	}
}
