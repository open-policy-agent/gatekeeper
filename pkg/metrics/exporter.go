package metrics

import (
	"flag"
	"fmt"
	"strings"
	"sync"

	"go.opencensus.io/stats/view"
	"sigs.k8s.io/controller-runtime/pkg/manager"
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

type Manager struct {
	mgr manager.Manager
}

func AddToManager(m manager.Manager) error {
	mm, err := New(m)
	if err != nil {
		return err
	}
	return m.Add(mm)
}

func New(mgr manager.Manager) (*Manager, error) {
	mm := &Manager{
		mgr: mgr,
	}
	return mm, nil
}

// Start implements the Runnable interface
func (mm *Manager) Start(stop <-chan struct{}) error {
	log.Info("Starting metrics manager")
	defer log.Info("Stopping metrics manager workers")
	errCh := make(chan error)
	go func() { errCh <- mm.newMetricsExporter() }()
	select {
	case <-stop:
		return nil
	case err := <-errCh:
		if err != nil {
			return err
		}
	}
	// We must block indefinitely or manager will exit
	<-stop
	return nil
}

func (mm *Manager) newMetricsExporter() error {
	ce := mm.getCurMetricsExporter()
	// If there is a Prometheus Exporter server running, stop it.
	resetCurPromSrv()

	if ce != nil {
		// UnregisterExporter is idempotent and it can be called multiple times for the same exporter
		// without side effects.
		view.UnregisterExporter(ce)
	}
	var e view.Exporter
	var err error
	mb := strings.ToLower(*metricsBackend)
	switch mb {
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

func (mm *Manager) getCurMetricsExporter() view.Exporter {
	metricsMux.RLock()
	defer metricsMux.RUnlock()
	return curMetricsExporter
}
