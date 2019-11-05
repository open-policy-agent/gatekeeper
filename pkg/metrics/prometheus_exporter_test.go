package metrics

import (
	"testing"
	"time"
)

func TestPrometheusExporter(t *testing.T) {
	expectedAddr := ":9090"

	_, err := newPrometheusExporter()
	if err != nil {
		t.Error(err)
	}

	time.Sleep(100 * time.Millisecond)
	srv := getCurPromSrv()
	if srv.Addr != expectedAddr {
		t.Errorf("Expected address %v but got %v", expectedAddr, srv.Addr)
	}
}
