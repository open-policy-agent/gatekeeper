package gator

import (
	"bytes"
	"fmt"
	"io"

	v1 "github.com/open-policy-agent/opa/v1/topdown/print"
)

const (
	// DefaultPrintBufferLimit caps how many bytes of print output gator keeps in memory.
	DefaultPrintBufferLimit int64 = 1 << 20 // 1 MiB

	printOutputTruncatedMsg = "\n... additional print output truncated ...\n"
)

// PrintBuffer is an in-memory writer with a fixed size limit.
type PrintBuffer struct {
	buffer    bytes.Buffer
	remaining int64
	truncated bool
}

// NewPrintBuffer creates a buffer that stores at most limit bytes.
func NewPrintBuffer(limit int64) *PrintBuffer {
	return &PrintBuffer{
		remaining: limit,
	}
}

// Write writes up to the configured limit and silently discards the rest.
func (b *PrintBuffer) Write(p []byte) (int, error) {
	if b.remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}

	toWrite := int64(len(p))
	if toWrite > b.remaining {
		toWrite = b.remaining
		b.truncated = true
	}

	_, err := b.buffer.Write(p[:toWrite])
	b.remaining -= toWrite

	return len(p), err
}

func (b *PrintBuffer) Len() int {
	return b.buffer.Len()
}

func (b *PrintBuffer) String() string {
	if b.truncated {
		return b.buffer.String() + printOutputTruncatedMsg
	}

	return b.buffer.String()
}

// PrintHook implements the OPA print hook interface to capture print statement output from Rego policies.
type PrintHook struct {
	writer io.Writer
}

// NewPrintHook creates and returns a new instance of PrintHook and writes to writer.
func NewPrintHook(writer io.Writer) PrintHook {
	return PrintHook{writer}
}

// Print writes message to writer passed to PrintHook when it was created.
func (h PrintHook) Print(ctx v1.Context, message string) error {
	_, err := fmt.Fprintln(h.writer, message)
	return err
}
