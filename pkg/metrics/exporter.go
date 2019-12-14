package metrics

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"go.opencensus.io/stats/view"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	metricsBackend = flag.String("metrics-backend", "Prometheus", "Backend used for metrics")
	prometheusPort = flag.Int("prometheus-port", 8888, "Prometheus port for metrics backend")
)

const prometheusExporter = "prometheus"

var _ manager.Runnable = &runner{}

type runner struct {
	mgr manager.Manager
}

func AddToManager(m manager.Manager) error {
	mr, err := new(m)
	if err != nil {
		return err
	}
	return m.Add(mr)
}

func new(mgr manager.Manager) (*runner, error) {
	mr := &runner{
		mgr: mgr,
	}
	return mr, nil
}

// Start implements the Runnable interface
func (r *runner) Start(stop <-chan struct{}) error {
	log.Info("Starting metrics runner")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer log.Info("Stopping metrics runner workers")
	errCh := make(chan error)
	go func() { errCh <- r.newMetricsExporter() }()
	select {
	case <-stop:
		return r.shutdownMetricsExporter(ctx)
	case err := <-errCh:
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) newMetricsExporter() error {
	var e view.Exporter
	var err error
	mb := strings.ToLower(*metricsBackend)
	log.Info("metrics", "backend", mb)
	switch mb {
	// Prometheus is the only exporter for now
	case prometheusExporter:
		err = newPrometheusExporter()
	default:
		err = fmt.Errorf("unsupported metrics backend %v", *metricsBackend)
	}
	if err != nil {
		return err
	}
	view.RegisterExporter(e)
	return nil
}

func (r *runner) shutdownMetricsExporter(ctx context.Context) error {
	mb := strings.ToLower(*metricsBackend)
	switch mb {
	case prometheusExporter:
		log.Info("shutting down prometheus server")
		if curPromSrv != nil {
			if err := curPromSrv.Shutdown(ctx); err != nil {
				return err
			}
		}
		return nil
	default:
		log.Info("nothing to shutdown for unsupported metrics backend %v", *metricsBackend)
		return nil
	}
}
