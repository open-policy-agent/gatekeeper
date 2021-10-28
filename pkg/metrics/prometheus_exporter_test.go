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

		// TODO(willbeason): newPrometheusExporter() never exits, so the rest of the code in this goroutine never
		//  executes. As this goroutine is asynchronous with the actual test, the test runner doesn't wait for this
		//  goroutine to finish before exiting the test (implicitly canceling the goroutine). Unfortunately the fix
		//  to this bug is nontrivial, so I'll be doing it as its own pull request.
		// If you get this panic, it means you've fixed the bug.
		panic("THIS TEST DOES NOT WORK AS INTENDED")
	}()

	time.Sleep(100 * time.Millisecond)
	if curPromSrv.Addr != expectedAddr {
		t.Errorf("Expected address %v but got %v", expectedAddr, curPromSrv.Addr)
	}
}
