package gator

import (
	"fmt"
	v1 "github.com/open-policy-agent/opa/v1/topdown/print"
	"io"
)

type PrintHook struct {
	writer io.Writer
}

func NewPrintHook(writer io.Writer) PrintHook {
	return PrintHook{writer}
}

func (h PrintHook) Print(ctx v1.Context, string string) error {
	_, err := fmt.Fprintln(h.writer, string)
	return err
}
