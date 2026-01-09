package gator

import (
	"fmt"
	"io"

	v1 "github.com/open-policy-agent/opa/v1/topdown/print"
)

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
