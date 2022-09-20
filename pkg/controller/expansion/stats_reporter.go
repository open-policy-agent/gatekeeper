package expansion

import (
	"context"

	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"k8s.io/apimachinery/pkg/types"
)

const (
	etMetricName = "expansion_templates"
	etDesc       = "Number of observed expansion templates"
)

var (
	etM = stats.Int64(etMetricName, etDesc, stats.UnitDimensionless)

	views = []*view.View{
		{
			Name:        etMetricName,
			Measure:     etM,
			Description: etDesc,
			Aggregation: view.LastValue(),
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
	return &etRegistry{cache: make(map[types.NamespacedName]bool)}
}

type etRegistry struct {
	cache map[types.NamespacedName]bool
	dirty bool
}

func (r *etRegistry) add(key types.NamespacedName) {
	r.cache[key] = true
	r.dirty = true
}

func (r *etRegistry) remove(key types.NamespacedName) {
	delete(r.cache, key)
	r.dirty = true
}

func (r *etRegistry) report(ctx context.Context) error {
	if !r.dirty {
		return nil
	}

	if err := metrics.Record(ctx, etM.M(int64(len(r.cache)))); err != nil {
		r.dirty = false
		return err
	}

	return nil
}
