package metrics

import (
	"fmt"
	"net/http"

	"contrib.go.opencensus.io/exporter/prometheus"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var curPromSrv *http.Server

var log = logf.Log.WithName("metrics")

const namespace = "gatekeeper"

func newPrometheusExporter() error {
	e, err := prometheus.NewExporter(prometheus.Options{Namespace: namespace})
	if err != nil {
		log.Error(err, "Failed to create the Prometheus exporter.")
		return err
	}
	errCh := make(chan error)
	log.Info("Starting server for OpenCensus Prometheus exporter")
	// Start the server for Prometheus scraping
	srv := startNewPromSrv(e, *prometheusPort)
	errCh <- srv.ListenAndServe()
	err = <-errCh
	if err != nil {
		return err
	}
	return nil
}

func startNewPromSrv(e *prometheus.Exporter, port int) *http.Server {
	sm := http.NewServeMux()
	sm.Handle("/metrics", e)
	curPromSrv = &http.Server{
		Addr:    fmt.Sprintf(":%v", port),
		Handler: sm,
	}
	return curPromSrv
}
