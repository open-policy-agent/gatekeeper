package commons

import (
	"fmt"
	"os"
)

func ErrFatalf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}

func StringToFile(s, path string) {
	file, err := os.Create(path)
	if err != nil {
		ErrFatalf("error creating file at path %s: %v", path, err)
	}

	if _, err = fmt.Fprint(file, s); err != nil {
		ErrFatalf("error writing to file at path %s: %s", path, err)
	}
}
