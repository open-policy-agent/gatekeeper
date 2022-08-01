package opencensus

import (
	"context"

	"contrib.go.opencensus.io/exporter/ocagent"
	"go.opencensus.io/stats/view"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	Name         = "opencensus"
	metricPrefix = "gatekeeper"
)

var log = logf.Log.WithName("opencensus-exporter")

func Start(ctx context.Context) error {
	exporter, err := ocagent.NewExporter(ocagent.WithServiceName(metricPrefix))
	if err != nil {
		return err
	}
	view.RegisterExporter(exporter)
	defer func() {
		if err := exporter.Stop(); err != nil {
			log.Error(err, "failed to shut down the opencensus exporter")
		}
	}()

	<-ctx.Done()
	return nil
}
