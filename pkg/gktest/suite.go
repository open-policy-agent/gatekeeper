package gktest

import "io/fs"

// Suite defines a set of TestCases which all use the same ConstraintTemplate
// and Constraint.
type Suite struct {
	TestCases []TestCase
}

// Run executes every TestCase in the Suite. Returns the results for every
// TestCase.
func (s Suite) Run(f fs.FS, filter Filter) []Result {
	results := make([]Result, len(s.TestCases))
	for i, tc := range s.TestCases {
		if !filter.MatchesTest(tc) {
			continue
		}

		results[i] = tc.Run(f)
	}
	return results
}

// TestCase runs Constraint against a YAML object
type TestCase struct{}

// Run executes the TestCase and returns the Result of the run.
func (tc TestCase) Run(f fs.FS) Result {
	return Result{}
}
