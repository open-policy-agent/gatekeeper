package metrics

import (
	"testing"
	"time"
)

func TestPrometheusExporter(t *testing.T) {
	const expectedAddr = ":8888"

	e, err := newPrometheusExporter()
	if err != nil {
		t.Fatal(err)
	}
	if e == nil {
		t.Error("newPrometheusExporter() should not return nil")
	}

	srv := startPrometheusExporter(e)
	go func() {
		err = listenAndServe(srv)
		if err != nil {
			t.Error(err)
		}
	}()

	// TODO: This test should actually check that the exporter is able to serve requests.
	time.Sleep(100 * time.Millisecond)

	if curPromSrv.Addr != expectedAddr {
		t.Errorf("Expected address %v but got %v", expectedAddr, curPromSrv.Addr)
	}
}
