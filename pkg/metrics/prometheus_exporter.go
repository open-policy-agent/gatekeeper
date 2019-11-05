package metrics

import (
	"fmt"
	"net/http"
	"sync"

	"contrib.go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var (
	curPromSrv    *http.Server
	curPromSrvMux sync.Mutex
)

var log = logf.Log.WithName("metrics")

const namespace = "gatekeeper"

func newPrometheusExporter() (view.Exporter, error) {
	e, err := prometheus.NewExporter(prometheus.Options{Namespace: namespace})
	if err != nil {
		log.Error(err, "Failed to create the Prometheus exporter.")
		return nil, err
	}
	log.Info("Starting server for OpenCensus Prometheus exporter")
	// Start the server for Prometheus scraping
	go func() {
		srv := startNewPromSrv(e, *prometheusPort)
		srv.ListenAndServe()
	}()
	return e, nil
}

func getCurPromSrv() *http.Server {
	curPromSrvMux.Lock()
	defer curPromSrvMux.Unlock()
	return curPromSrv
}

func resetCurPromSrv() {
	curPromSrvMux.Lock()
	defer curPromSrvMux.Unlock()
	if curPromSrv != nil {
		curPromSrv.Close()
		curPromSrv = nil
	}
}

func startNewPromSrv(e *prometheus.Exporter, port int) *http.Server {
	sm := http.NewServeMux()
	sm.Handle("/metrics", e)
	curPromSrvMux.Lock()
	defer curPromSrvMux.Unlock()
	if curPromSrv != nil {
		curPromSrv.Close()
	}
	curPromSrv = &http.Server{
		Addr:    fmt.Sprintf(":%v", port),
		Handler: sm,
	}
	return curPromSrv
}
