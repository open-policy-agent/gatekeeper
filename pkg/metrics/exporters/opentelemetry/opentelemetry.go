package opentelemetry

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics/exporters/common"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/sdk/metric"
)

const (
	Name                          = "opentelemetry"
	defaultMetricsCollectInterval = 10 * time.Second
	defaultMetricsTimeout         = 30 * time.Second
)

var (
	otlpEndPoint   = flag.String("otlp-endpoint", "", "Opentelemetry exporter endpoint")
	metricInterval = flag.Duration("otlp-metric-interval", defaultMetricsCollectInterval, "interval to read metrics for opentelemetry exporter. defaulted to 10 secs if unspecified")
)

func Start(ctx context.Context) error {
	if *otlpEndPoint == "" {
		common.AddReader(nil)
		return fmt.Errorf("otlp-endpoint must be specified")
	}
	var err error
	exp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithInsecure(), otlpmetrichttp.WithEndpoint(*otlpEndPoint))
	if err != nil {
		common.AddReader(nil)
		return err
	}
	reader := metric.WithReader(metric.NewPeriodicReader(
		exp,
		metric.WithTimeout(defaultMetricsTimeout),
		metric.WithInterval(*metricInterval),
	))
	common.AddReader(reader)

	<-ctx.Done()
	return nil
}
