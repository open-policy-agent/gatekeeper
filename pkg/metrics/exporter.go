package metrics

import (
	"flag"
	"fmt"
	"strings"
	"sync"

	"go.opencensus.io/stats/view"
)

var (
	curMetricsExporter view.Exporter
	metricsMux         sync.RWMutex
)

var (
	metricsBackend = flag.String("metrics-backend", "Prometheus", "Backend used for metrics")
	prometheusPort = flag.Int("prometheus-port", 8888, "Prometheus port for metrics backend")
)

const prometheusExporter = "prometheus"

func NewMetricsExporter() error {
	ce := getCurMetricsExporter()
	// If there is a Prometheus Exporter server running, stop it.
	resetCurPromSrv()

	if ce != nil {
		// UnregisterExporter is idempotent and it can be called multiple times for the same exporter
		// without side effects.
		view.UnregisterExporter(ce)
	}
	var e view.Exporter
	var err error
	m := strings.ToLower(*metricsBackend)
	switch m {
	// Prometheus is the only exporter for now
	case prometheusExporter:
		e, err = newPrometheusExporter()
	default:
		err = fmt.Errorf("unsupported metrics backend %v", *metricsBackend)
	}
	if err != nil {
		return err
	}

	metricsMux.Lock()
	defer metricsMux.Unlock()
	view.RegisterExporter(e)
	curMetricsExporter = e

	return nil
}

func getCurMetricsExporter() view.Exporter {
	metricsMux.RLock()
	defer metricsMux.RUnlock()
	return curMetricsExporter
}
