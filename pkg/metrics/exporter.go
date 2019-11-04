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
	metricsBackend = flag.String("metricsBackend", "Prometheus", "Backend used for metrics")
	prometheusPort = flag.Int("prometheusPort", 9090, "Prometheus port")
)

const prometheusExporter = "prometheus"

func NewMetricsExporter() (view.Exporter, error) {
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
		return nil, err
	}
	return e, nil
}

func getCurMetricsExporter() view.Exporter {
	metricsMux.RLock()
	defer metricsMux.RUnlock()
	return curMetricsExporter
}

func SetCurMetricsExporter(e view.Exporter) {
	metricsMux.Lock()
	defer metricsMux.Unlock()
	view.RegisterExporter(e)
	curMetricsExporter = e
}
