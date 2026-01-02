package constraint

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
	constraintsMetricName = "constraints"
	vapbMetricName        = "validating_admission_policy_bindings"
	enforcementActionKey  = "enforcement_action"
	statusKey             = "status"
)

// vapbRegistry tracks individual VAPB resources for accurate counting.
type vapbRegistry struct {
	mu    sync.RWMutex
	cache map[types.NamespacedName]metrics.VAPStatus
}

func newVAPBRegistry() *vapbRegistry {
	return &vapbRegistry{cache: make(map[types.NamespacedName]metrics.VAPStatus)}
}

func (r *vapbRegistry) add(key types.NamespacedName, status metrics.VAPStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.cache[key]
	if ok && existing == status {
		return
	}
	r.cache[key] = status
}

func (r *vapbRegistry) remove(key types.NamespacedName) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cache, key)
}

func (r *vapbRegistry) computeTotals() map[metrics.VAPStatus]int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	totals := make(map[metrics.VAPStatus]int64)
	for _, status := range r.cache {
		totals[status]++
	}
	return totals
}

func (r *reporter) observeConstraints(_ context.Context, observer metric.Int64Observer) error {
	r.mux.RLock()
	defer r.mux.RUnlock()
	for t, v := range r.constraintsReport {
		observer.Observe(v, metric.WithAttributes(attribute.String(enforcementActionKey, string(t.enforcementAction)), attribute.String(statusKey, string(t.status))))
	}
	return nil
}

func (r *reporter) observeVAPB(_ context.Context, observer metric.Int64Observer) error {
	totals := r.vapbRegistry.computeTotals()
	for _, status := range metrics.AllVAPStatuses {
		observer.Observe(totals[status], metric.WithAttributes(attribute.String(statusKey, string(status))))
	}
	return nil
}

func (r *reporter) reportConstraints(_ context.Context, t tags, v int64) error {
	r.mux.Lock()
	defer r.mux.Unlock()
	if r.constraintsReport == nil {
		r.constraintsReport = make(map[tags]int64)
	}
	r.constraintsReport[t] = v
	return nil
}

// StatsReporter reports audit metrics.
type StatsReporter interface {
	reportConstraints(ctx context.Context, t tags, v int64) error
	ReportVAPBStatus(ctx context.Context, name types.NamespacedName, status metrics.VAPStatus) error
	DeleteVAPBStatus(ctx context.Context, name types.NamespacedName) error
}

// newStatsReporter creates a reporter for audit metrics.
func newStatsReporter() (*reporter, error) {
	r := &reporter{vapbRegistry: newVAPBRegistry()}
	var err error
	meter := otel.GetMeterProvider().Meter("gatekeeper")
	_, err = meter.Int64ObservableGauge(
		constraintsMetricName,
		metric.WithDescription("Current number of known constraints"), metric.WithInt64Callback(r.observeConstraints))
	if err != nil {
		return nil, err
	}
	_, err = meter.Int64ObservableGauge(
		vapbMetricName,
		metric.WithDescription("Number of ValidatingAdmissionPolicyBinding resources by generation status (active = successfully generated, error = generation failed)"),
		metric.WithInt64Callback(r.observeVAPB),
	)
	if err != nil {
		return nil, err
	}
	return r, nil
}

type reporter struct {
	mux               sync.RWMutex
	constraintsReport map[tags]int64
	vapbRegistry      *vapbRegistry
}

func (r *reporter) ReportVAPBStatus(_ context.Context, name types.NamespacedName, status metrics.VAPStatus) error {
	r.vapbRegistry.add(name, status)
	return nil
}

func (r *reporter) DeleteVAPBStatus(_ context.Context, name types.NamespacedName) error {
	r.vapbRegistry.remove(name)
	return nil
}
