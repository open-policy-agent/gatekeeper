package utils

import (
	"fmt"
	"os"
)

func ErrFatalF(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format, a...)
	os.Exit(1)
}
