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

func (r *reporter) observeConstraints(_ context.Context, observer metric.Int64Observer) error {
	r.mux.RLock()
	defer r.mux.RUnlock()
	for t, v := range r.constraintsReport {
		observer.Observe(v, metric.WithAttributes(attribute.String(enforcementActionKey, string(t.enforcementAction)), attribute.String(statusKey, string(t.status))))
	}
	return nil
}

func (r *reporter) observeVAPB(_ context.Context, observer metric.Int64Observer) error {
	totals := r.vapbRegistry.ComputeTotals()
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
	ReportVAPBStatus(name types.NamespacedName, status metrics.VAPStatus)
	DeleteVAPBStatus(name types.NamespacedName)
}

// newStatsReporter creates a reporter for audit metrics.
func newStatsReporter() (*reporter, error) {
	r := &reporter{vapbRegistry: metrics.NewVAPStatusRegistry()}
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
	vapbRegistry      *metrics.VAPStatusRegistry
}

func (r *reporter) ReportVAPBStatus(name types.NamespacedName, status metrics.VAPStatus) {
	r.vapbRegistry.Add(name, status)
}

func (r *reporter) DeleteVAPBStatus(name types.NamespacedName) {
	r.vapbRegistry.Remove(name)
}
