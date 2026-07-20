package externaldata

import (
	"context"
	"sync"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"k8s.io/apimachinery/pkg/types"
)

const (
	providerMetricName     = "providers"
	providerErrorCountName = "provider_error_count"
	statusKey              = "status"

	providerDesc      = "Number of external data providers by status"
	providerErrorDesc = "Incremental counter for all provider errors occurring over time"
)

func (r *reporter) observeProviderMetric(_ context.Context, o metric.Int64Observer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for status, count := range r.statusReport {
		o.Observe(count, metric.WithAttributes(attribute.String(statusKey, string(status))))
	}
	return nil
}

func (r *reporter) observeProviderErrorCount(_ context.Context, o metric.Int64Observer) error {
	// Always observe so the metric is exported even when no errors have occurred yet.
	// A plain Int64Counter is only exported after the first Add(), which made
	// gatekeeper_provider_error_count appear missing on healthy clusters.
	o.Observe(externaldata.ProviderErrorCount())
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

	// Register the gatekeeper_provider_error_count counter metric as an
	// observable counter so it is always present on the metrics endpoint.
	_, err = meter.Int64ObservableCounter(
		providerErrorCountName,
		metric.WithDescription(providerErrorDesc),
		metric.WithInt64Callback(r.observeProviderErrorCount),
	)
	if err != nil {
		panic(err)
	}

	return r
}

// reportProviderError increments the provider error counter.
func (r *reporter) reportProviderError(_ context.Context) {
	externaldata.ReportProviderError()
}

type reporter struct {
	mu           sync.RWMutex
	cache        map[types.NamespacedName]metrics.Status
	dirty        bool
	statusReport map[metrics.Status]int64
}

func (r *reporter) add(key types.NamespacedName, status metrics.Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.cache[key]
	if ok && v == status {
		return
	}
	r.cache[key] = status
	r.dirty = true
}

func (r *reporter) remove(key types.NamespacedName) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.cache[key]; !exists {
		return
	}
	delete(r.cache, key)
	r.dirty = true
}

func (r *reporter) report(_ context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.dirty {
		return
	}

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

	r.dirty = false
}
