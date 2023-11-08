package stackdriver

import (
	"context"
	"flag"
	"time"

	traceapi "cloud.google.com/go/trace/apiv2"
	stackdriver "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics/exporters/view"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"golang.org/x/oauth2/google"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	Name                          = "stackdriver"
	metricPrefix                  = "custom.googleapis.com/opentelemetry/gatekeeper/"
	defaultMetricsCollectInterval = 10
)

var (
	ignoreMissingCreds = flag.Bool("stackdriver-only-when-available", false, "Only attempt to start the stackdriver exporter if credentials are available")
	metricInterval     = flag.Uint("stackdriver-metric-interval", defaultMetricsCollectInterval, "interval to read metrics for stackdriver exporter. defaulted to 10 secs if unspecified")
	log                = logf.Log.WithName("stackdriver-exporter")
)

func Start(ctx context.Context) error {
	// Verify that default stackdriver credentials are available
	if _, err := google.FindDefaultCredentials(ctx, traceapi.DefaultAuthScopes()...); err != nil {
		if *ignoreMissingCreds {
			log.Error(err, "Missing credentials, cannot start stackdriver exporter")
			return nil
		}
		return err
	}

	e, err := stackdriver.New(stackdriver.WithProjectID(metricPrefix))
	if err != nil {
		if *ignoreMissingCreds {
			log.Error(err, "Error initializing stackdriver exporter, not exporting stackdriver metrics")
			return nil
		}
		return err
	}
	reader := metric.NewPeriodicReader(e, metric.WithInterval(time.Duration(*metricInterval)*time.Second))
	meterProvider := metric.NewMeterProvider(
		metric.WithReader(reader),
		metric.WithView(view.Views()...),
	)

	otel.SetMeterProvider(meterProvider)
	otel.SetLogger(logf.Log.WithName("metrics"))

	<-ctx.Done()
	return nil
}
