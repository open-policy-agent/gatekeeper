package expansion

import (
	"context"
	"sync"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"k8s.io/apimachinery/pkg/types"
)

const (
	etMetricName = "expansion_templates"
	etDesc       = "Number of observed expansion templates"
	statusKey    = "status"
)

func newRegistry() *etRegistry {
	r := &etRegistry{cache: make(map[types.NamespacedName]metrics.Status)}
	var err error
	meter := otel.GetMeterProvider().Meter("gatekeeper")
	_, err = meter.Int64ObservableGauge(
		etMetricName,
		metric.WithDescription(etDesc),
		metric.WithInt64Callback(r.observeETM))
	if err != nil {
		panic(err)
	}
	return r
}

type etRegistry struct {
	mu           sync.RWMutex
	cache        map[types.NamespacedName]metrics.Status
	dirty        bool
	statusReport map[metrics.Status]int64
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

func (r *etRegistry) report(_ context.Context) {
	if !r.dirty {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.statusReport == nil {
		r.statusReport = make(map[metrics.Status]int64)
	}

	totals := make(map[metrics.Status]int64)
	for _, status := range r.cache {
		totals[status]++
	}

	for _, s := range metrics.AllStatuses {
		r.statusReport[s] = totals[s]
	}
}

func (r *etRegistry) observeETM(_ context.Context, o metric.Int64Observer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for s, v := range r.statusReport {
		o.Observe(v, metric.WithAttributes(attribute.String(statusKey, string(s))))
	}
	return nil
}
