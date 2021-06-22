package gktest

// Result is the structured form of the results of running a test.
type Result struct{}

func (r Result) String() string {
	return ""
}

// IsFailure returns true if either a test failed or there was a problem
// executing tests.
func (r Result) IsFailure() bool {
	return false
}
