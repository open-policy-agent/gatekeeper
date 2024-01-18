package stackdriver

import (
	"context"
	"flag"
	"fmt"
	"time"

	traceapi "cloud.google.com/go/trace/apiv2"
	stackdriver "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics/exporters/common"
	"go.opentelemetry.io/contrib/detectors/aws/ec2"
	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	"golang.org/x/oauth2/google"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	Name                          = "stackdriver"
	metricPrefix                  = "custom.googleapis.com/opencensus/gatekeeper"
	defaultMetricsCollectInterval = 10 * time.Second
)

var (
	ignoreMissingCreds = flag.Bool("stackdriver-only-when-available", false, "Only attempt to start the stackdriver exporter if credentials are available")
	metricInterval     = flag.Duration("stackdriver-metric-interval", defaultMetricsCollectInterval, "interval to read metrics for stackdriver exporter. defaulted to 10 secs if unspecified")
	log                = logf.Log.WithName("stackdriver-exporter")
)

func Start(ctx context.Context) error {
	// Verify that default stackdriver credentials are available
	if _, err := google.FindDefaultCredentials(ctx, traceapi.DefaultAuthScopes()...); err != nil {
		if *ignoreMissingCreds {
			common.AddReader(nil)
			log.Error(err, "Missing credentials, cannot start stackdriver exporter")
			return nil
		}
		common.AddReader(nil)
		return err
	}

	e, err := stackdriver.New(stackdriver.WithMetricDescriptorTypeFormatter(func(desc metricdata.Metrics) string {
		return fmt.Sprintf("%s/%s", metricPrefix, desc.Name)
	}))
	if err != nil {
		if *ignoreMissingCreds {
			log.Error(err, "Error initializing stackdriver exporter, not exporting stackdriver metrics")
			return nil
		}
		common.AddReader(nil)
		return err
	}
	res, err := resource.New(ctx,
		resource.WithDetectors(gcp.NewDetector(), ec2.NewResourceDetector()),
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
	)
	if err != nil {
		common.AddReader(nil)
		return err
	}
	reader := metric.NewPeriodicReader(e, metric.WithInterval(*metricInterval))
	common.SetResource(res)
	common.AddReader(metric.WithReader(reader))

	<-ctx.Done()
	return nil
}
