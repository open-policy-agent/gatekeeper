package errors

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrorMap is a map from targets to the error the target returned.
type ErrorMap map[string]error

// Error implements error.
//
// Uses a pointer receiver to avoid potential errors.Is() bugs.
func (e *ErrorMap) Error() string {
	b := &strings.Builder{}

	// Make printed error deterministic by sorting keys.
	keys := make([]string, len(*e))
	i := 0
	for k := range *e {
		keys[i] = k
		i++
	}
	sort.Strings(keys)

	for _, k := range keys {
		b.WriteString(fmt.Sprintf("%s: %s\n", k, (*e)[k]))
	}
	return b.String()
}

func (e *ErrorMap) Is(target error) bool {
	t, ok := target.(*ErrorMap)
	if !ok {
		return false
	}

	if len(*e) != len(*t) {
		return false
	}

	for k := range *e {
		if !errors.Is((*e)[k], (*t)[k]) {
			return false
		}
	}

	return true
}

func (e *ErrorMap) Add(key string, err error) {
	(*e)[key] = err
}
