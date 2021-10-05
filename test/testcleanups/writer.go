package testcleanups

import (
	"io"
	"strings"
	"testing"
)

type Writer struct {
	t *testing.T
}

var _ io.Writer = &Writer{}

func NewTestWriter(t *testing.T) *Writer {
	return &Writer{t: t}
}

func (w *Writer) Write(p []byte) (n int, err error) {
	w.t.Helper()
	pstr := string(p)
	pstr = strings.TrimSpace(pstr)
	w.t.Log(pstr)

	return len(p), nil
}
