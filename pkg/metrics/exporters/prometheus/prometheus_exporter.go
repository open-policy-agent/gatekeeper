package prometheus

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics/exporters/view"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	Name              = "prometheus"
	namespace         = "gatekeeper"
	readHeaderTimeout = 60 * time.Second
)

var (
	log            = logf.Log.WithName("prometheus-exporter")
	prometheusPort = flag.Int("prometheus-port", 8888, "Prometheus port for metrics backend")
)

func Start(ctx context.Context) error {
	e, err := prometheus.New(
		prometheus.WithNamespace(namespace),
		prometheus.WithoutScopeInfo(),
	)
	if err != nil {
		return err
	}
	meterProvider := metric.NewMeterProvider(
		metric.WithReader(e),
		metric.WithView(view.Views()...),
	)
	server := newPromSrv(*prometheusPort)
	otel.SetMeterProvider(meterProvider)
	otel.SetLogger(logf.Log.WithName("metrics"))

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

func newPromSrv(port int) *http.Server {
	sm := http.NewServeMux()
	sm.Handle("/metrics", promhttp.Handler())
	server := &http.Server{
		Addr:              fmt.Sprintf(":%v", port),
		Handler:           sm,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	return server
}
