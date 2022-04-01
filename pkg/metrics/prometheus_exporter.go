package metrics

import (
	"fmt"
	"net/http"
	"sync"

	"contrib.go.opencensus.io/exporter/prometheus"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	ctlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

type prometheusServer struct {
	srv *http.Server
	mux sync.RWMutex
}

var curPromSrv = &prometheusServer{}

var log = logf.Log.WithName("metrics")

const namespace = "gatekeeper"

func newPrometheusExporter() (*prometheus.Exporter, error) {
	e, err := prometheus.NewExporter(prometheus.Options{
		Namespace:  namespace,
		Registerer: ctlmetrics.Registry,
		Gatherer:   ctlmetrics.Registry,
	})
	if err != nil {
		log.Error(err, "Failed to create the Prometheus exporter.")
		return nil, err
	}
	return e, nil
}

func startPrometheusExporter(e http.Handler) *http.Server {
	log.Info("Starting server for OpenCensus Prometheus exporter")
	// Start the server for Prometheus scraping
	return startNewPromSrv(e, *prometheusPort)
}

func listenAndServe(srv *http.Server) error {
	errCh := make(chan error)
	errCh <- srv.ListenAndServe()
	err := <-errCh
	if err != nil {
		return err
	}
	return nil
}

func startNewPromSrv(e http.Handler, port int) *http.Server {
	sm := http.NewServeMux()
	sm.Handle("/metrics", e)
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%v", port),
		Handler: sm,
	}
	curPromSrv.SetSrv(srv)
	return srv
}

func (p *prometheusServer) Srv() *http.Server {
	p.mux.RLock()
	defer p.mux.RUnlock()
	return p.srv
}

func (p *prometheusServer) SetSrv(srv *http.Server) {
	p.mux.Lock()
	defer p.mux.Unlock()
	p.srv = srv
}
