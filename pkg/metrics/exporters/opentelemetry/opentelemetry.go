package opentelemetry

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics/exporters/view"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/sdk/metric"
)

const (
	Name                          = "opentelemetry"
	metricPrefix                  = "gatekeeper"
	defaultMetricsCollectInterval = 10 * time.Second
	defaultMetricsTimeout         = 30 * time.Second
)

var (
	otlpEndPoint   = flag.String("otlp-endpoint", "", "Opentelemetry exporter endpoint")
	metricInterval = flag.Duration("otlp-metric-interval", defaultMetricsCollectInterval, "interval to read metrics for opentelemetry exporter. defaulted to 10 secs if unspecified")
)

func Start(ctx context.Context) error {
	if *otlpEndPoint == "" {
		return fmt.Errorf("otlp-endpoint must be specified")
	}
	exp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithInsecure(), otlpmetrichttp.WithEndpoint(*otlpEndPoint))
	if err != nil {
		return err
	}
	meterProvider := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(
			exp,
			metric.WithTimeout(defaultMetricsTimeout),
			metric.WithInterval(*metricInterval),
		)),
		metric.WithView(view.Views()...),
	)

	otel.SetMeterProvider(meterProvider)
	defer func() {
		if err := meterProvider.Shutdown(ctx); err != nil {
			panic(err)
		}
	}()

	<-ctx.Done()
	return nil
}
