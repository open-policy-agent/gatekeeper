package externaldata

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
	providerMetricName      = "providers"
	providerErrorCountName  = "provider_error_count"
	statusKey              = "status"

	providerDesc      = "Number of external data providers by status"
	providerErrorDesc = "Incremental counter for all provider errors occurring over time"
)

var (
	providerErrorCountM metric.Int64Counter
)

func (r *reporter) observeProviderMetric(_ context.Context, o metric.Int64Observer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.dirty {
		return nil
	}
	for status, count := range r.statusReport {
		o.Observe(count, metric.WithAttributes(attribute.String(statusKey, string(status))))
	}
	r.dirty = false
	return nil
}

// newStatsReporter creates a reporter for external data provider metrics.
func newStatsReporter() *reporter {
	var err error
	r := &reporter{
		cache: make(map[types.NamespacedName]metrics.Status),
	}
	meter := otel.GetMeterProvider().Meter("gatekeeper")

	// Register the gatekeeper_providers gauge metric
	_, err = meter.Int64ObservableGauge(
		providerMetricName,
		metric.WithDescription(providerDesc),
		metric.WithInt64Callback(r.observeProviderMetric),
	)
	if err != nil {
		panic(err)
	}

	// Register the gatekeeper_provider_error_count counter metric
	providerErrorCountM, err = meter.Int64Counter(
		providerErrorCountName,
		metric.WithDescription(providerErrorDesc),
	)
	if err != nil {
		panic(err)
	}

	return r
}

// reportProviderError increments the provider error counter with the specific error type.
func (r *reporter) reportProviderError(ctx context.Context) {
	providerErrorCountM.Add(ctx, 1)
}

type reporter struct {
	mu           sync.RWMutex
	cache        map[types.NamespacedName]metrics.Status
	dirty        bool
	statusReport map[metrics.Status]int64
}

func (r *reporter) add(key types.NamespacedName, status metrics.Status) {
	v, ok := r.cache[key]
	if ok && v == status {
		return
	}
	r.cache[key] = status
	r.dirty = true
}

func (r *reporter) remove(key types.NamespacedName) {
	if _, exists := r.cache[key]; !exists {
		return
	}
	delete(r.cache, key)
	r.dirty = true
}

func (r *reporter) report(_ context.Context) {
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
