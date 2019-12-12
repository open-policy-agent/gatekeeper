package metrics

import (
	"testing"
	"time"
)

func TestPrometheusExporter(t *testing.T) {
	const expectedAddr = ":8888"

	go func() {
		e, err := newPrometheusExporter()
		if err != nil {
			t.Error(err)
		}
		if e == nil {
			t.Error("expected prometheus exporter but got nil")
		}
	}()

	time.Sleep(100 * time.Millisecond)
	srv := getCurPromSrv()
	if srv.Addr != expectedAddr {
		t.Errorf("Expected address %v but got %v", expectedAddr, srv.Addr)
	}
}
