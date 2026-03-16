//nolint:revive // Package name intentionally conflicts with stdlib; use alias "clienterrors" when importing.
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
		fmt.Fprintf(b, "%s: %s\n", k, (*e)[k])
	}
	return b.String()
}

// Is implements error comparison for ErrorMap.
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

// Add adds an error to the ErrorMap for the given key.
func (e *ErrorMap) Add(key string, err error) {
	(*e)[key] = err
}
