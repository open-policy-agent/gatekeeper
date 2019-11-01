package client

import "strings"

// Errors is a list of error.
type Errors []error

// Errors implements error
var _ error = Errors{}

// Error implements error.
func (errs Errors) Error() string {
	s := make([]string, len(errs))
	for _, e := range errs {
		s = append(s, e.Error())
	}
	return strings.Join(s, "\n")
}
