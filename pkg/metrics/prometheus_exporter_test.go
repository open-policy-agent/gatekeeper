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
			t.Error("newPrometheusExporter() should not return nil")
		}
	}()

	time.Sleep(100 * time.Millisecond)
	if curPromSrv.Addr != expectedAddr {
		t.Errorf("Expected address %v but got %v", expectedAddr, curPromSrv.Addr)
	}
}
