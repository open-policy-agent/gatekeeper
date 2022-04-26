package prometheus

import (
	"context"
	"flag"
	"fmt"
	"net/http"

	"contrib.go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	ctlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	Name      = "prometheus"
	namespace = "gatekeeper"
)

var (
	log            = logf.Log.WithName("prometheus-exporter")
	prometheusPort = flag.Int("prometheus-port", 8888, "Prometheus port for metrics backend")
)

func Start(ctx context.Context) error {
	e, err := newExporter()
	if err != nil {
		return err
	}
	view.RegisterExporter(e)

	server := newPromSrv(e, *prometheusPort)
	errCh := make(chan error)
	srv := func() {
		err := server.ListenAndServe()
		errCh <- err
	}
	go srv()
	select {
	case <-ctx.Done():
		log.Info("shutting down prometheus server")
		if err := server.Shutdown(ctx); err != nil {
			return err
		}
	case err := <-errCh:
		if err != nil {
			return err
		}
	}
	return nil
}

func newExporter() (*prometheus.Exporter, error) {
	e, err := prometheus.NewExporter(prometheus.Options{
		Namespace:  namespace,
		Registerer: ctlmetrics.Registry,
		Gatherer:   ctlmetrics.Registry,
	})
	if err != nil {
		log.Error(err, "Failed to create the Prometheus exporter")
		return nil, err
	}
	return e, nil
}

func newPromSrv(e http.Handler, port int) *http.Server {
	sm := http.NewServeMux()
	sm.Handle("/metrics", e)
	curPromSrv := &http.Server{
		Addr:    fmt.Sprintf(":%v", port),
		Handler: sm,
	}
	return curPromSrv
}
