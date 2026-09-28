package gator

import (
	"strings"
	"testing"
)

func TestPrintBuffer_Truncates(t *testing.T) {
	t.Parallel()

	b := NewPrintBuffer(5)

	_, err := b.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	if got, want := b.Len(), 5; got != want {
		t.Fatalf("len: got %d, want %d", got, want)
	}

	got := b.String()
	if !strings.Contains(got, "hello") {
		t.Fatalf("string missing buffered data: %q", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("string missing truncation marker: %q", got)
	}
}
