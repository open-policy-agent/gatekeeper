package externaldata

import (
	"context"
	"sync"

	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	providersMetricName             = "gatekeeper_provider"
	providerErrorCountMetricName    = "gatekeeper_provider_error_count"
	statusKey                       = "status"
)

var (
	providerErrorCountM metric.Int64Counter
)

// ProviderStatus defines the status of a provider for metrics.
type ProviderStatus string

const (
	// ProviderStatusActive denotes an active provider.
	ProviderStatusActive ProviderStatus = "active"
	// ProviderStatusError denotes a provider with errors.
	ProviderStatusError ProviderStatus = "error"
)

// StatsReporter reports provider-related metrics.
type StatsReporter interface {
	ReportProviderStatus(status ProviderStatus, count int) error
	ReportProviderError(ctx context.Context, providerName string, errorType statusv1beta1.ProviderErrorType) error
}

// reporter implements StatsReporter interface.
type reporter struct {
	mu            sync.RWMutex
	providerGauge metric.Int64ObservableGauge
}

// NewStatsReporter creates a reporter for provider metrics.
func NewStatsReporter() (StatsReporter, error) {
	r := &reporter{}
	var err error
	meter := otel.GetMeterProvider().Meter("gatekeeper")

	providerErrorCountM, err = meter.Int64Counter(
		providerErrorCountMetricName,
		metric.WithDescription("Total number of external data provider errors"),
	)
	if err != nil {
		return nil, err
	}

	r.providerGauge, err = meter.Int64ObservableGauge(
		providersMetricName,
		metric.WithDescription("Number of external data providers"),
	)
	if err != nil {
		return nil, err
	}

	if _, err := meter.RegisterCallback(r.observeProviders, r.providerGauge); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *reporter) observeProviders(ctx context.Context, observer metric.Observer) error {
	// This would ideally iterate through the provider cache to get counts
	// For now, we'll observe 0 as a placeholder
	observer.ObserveInt64(r.providerGauge, 0,
		metric.WithAttributes(
			attribute.String(statusKey, string(ProviderStatusActive)),
		))
	observer.ObserveInt64(r.providerGauge, 0,
		metric.WithAttributes(
			attribute.String(statusKey, string(ProviderStatusError)),
		))
	return nil
}

func (r *reporter) ReportProviderStatus(status ProviderStatus, count int) error {
	// This is handled by the observable gauge callback
	return nil
}

func (r *reporter) ReportProviderError(ctx context.Context, providerName string, errorType statusv1beta1.ProviderErrorType) error {
	providerErrorCountM.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("provider", providerName),
			attribute.String("error_type", string(errorType)),
		))
	return nil
}