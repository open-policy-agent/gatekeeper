package utils

import (
	"fmt"
	"os"
)

func ErrFatalf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
