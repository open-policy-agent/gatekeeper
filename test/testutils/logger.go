package testutils

import (
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// NewLogger creates a logger specifically for t which logs directly to the test.
// Use test-specific loggers so that when tests fail, only the log messages from the offending test are printed rather
// than log messages for every test in the package.
func NewLogger(t *testing.T) logr.Logger {
	return zap.New(zap.UseDevMode(true), zap.WriteTo(NewTestWriter(t)))
}

type Writer struct {
	t *testing.T

	// stopped tracks whether the test has ended. In this case, further attempts to write log messages will fail and
	// cause flaky panics in unrelated tests. We expect managers to send log messages even after shutting down as there
	// is no guarantee of the order of execution of cancellation logic and the manager shutting down. Still-executing
	// functions will return errors such as "context canceled", but these are not relevant to the test.
	stopped    bool
	stoppedMtx sync.RWMutex
}

var _ io.Writer = &Writer{}

func NewTestWriter(t *testing.T) *Writer {
	w := &Writer{t: t}
	t.Cleanup(func() {
		w.stoppedMtx.Lock()
		w.stopped = true
		w.stoppedMtx.Unlock()
	})

	return w
}

func (w *Writer) Write(p []byte) (n int, err error) {
	w.t.Helper()

	w.stoppedMtx.RLock()
	stopped := w.stopped
	w.stoppedMtx.RUnlock()

	if stopped {
		// The test has completed and we shouldn't log anything else to the test runner.
		return 0, os.ErrClosed
	}

	pstr := string(p)
	pstr = strings.TrimSpace(pstr)

	// t.Log is threadsafe.
	w.t.Log(pstr)

	return len(p), nil
}
