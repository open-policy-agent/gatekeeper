package stackdriver

import (
	"context"
	"flag"

	traceapi "cloud.google.com/go/trace/apiv2"
	"contrib.go.opencensus.io/exporter/stackdriver"
	"contrib.go.opencensus.io/exporter/stackdriver/monitoredresource"
	"golang.org/x/oauth2/google"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	Name         = "stackdriver"
	metricPrefix = "custom.googleapis.com/opencensus/gatekeeper/"
)

var (
	ignoreMissingCreds = flag.Bool("stackdriver-only-when-available", false, "Only attempt to start the stackdriver exporter if credentials are available")
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

	exporter, err := stackdriver.NewExporter(stackdriver.Options{
		MetricPrefix:      metricPrefix,
		MonitoredResource: monitoredresource.Autodetect(),
	})
	if err != nil {
		return err
	}

	if err := exporter.StartMetricsExporter(); err != nil {
		return err
	}
	defer exporter.StopMetricsExporter()

	<-ctx.Done()
	return nil
}
