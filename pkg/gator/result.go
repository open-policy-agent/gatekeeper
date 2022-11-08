package gator

import (
	"fmt"
	"time"
)

// Duration is an alias of time.Duration to allow for custom formatting.
// Otherwise time formatting must be done inline everywhere.
type Duration time.Duration

func (d Duration) String() string {
	return fmt.Sprintf("%.3fs", time.Duration(d).Seconds())
}

// SuiteResult is the Result of running a Suite of tests.
type SuiteResult struct {
	// Path is the absolute path to the file which defines Suite.
	Path string

	// Error is the error which stopped the Suite from executing.
	// If defined, TestResults is empty.
	Error error

	// Runtime is the time it took for this Suite of tests to run.
	Runtime Duration

	// TestResults are the results of running the tests for each defined
	// Template/Constraint pair.
	TestResults []TestResult

	// Skipped is whether this Suite was skipped and not run.
	Skipped bool
}

// IsFailure returns true if there was a problem running the Suite, or one of the
// Constraint tests failed.
func (r *SuiteResult) IsFailure() bool {
	if r.Error != nil {
		return true
	}
	for _, result := range r.TestResults {
		if result.IsFailure() {
			return true
		}
	}
	return false
}

// TestResult is the results of:
// 1) Compiling the ConstraintTemplate,
// 2) Instantiating the Constraint, and
// 3) Running all Tests defined for the Constraint.
type TestResult struct {
	// Name is the name given to the Template/Constraint pair under test.
	Name string

	// Skipped is whether this Test was skipped while running its parent Suite.
	Skipped bool

	// Error is the error which prevented running tests for this Constraint.
	// If defined, CaseResults is empty.
	Error error

	// Runtime is the time it took for the Template/Constraint to be compiled, and
	// the test Cases to run.
	Runtime Duration

	// CaseResults are individual results for all tests defined for this Constraint.
	CaseResults []CaseResult
}

// IsFailure returns true if there was a problem running the Constraint tests,
// or one of its Tests failed.
func (r *TestResult) IsFailure() bool {
	if r.Error != nil {
		return true
	}
	for _, result := range r.CaseResults {
		if result.IsFailure() {
			return true
		}
	}
	return false
}

// CaseResult is the result of evaluating a Constraint against a kubernetes
// object, and comparing the result with the expected result.
type CaseResult struct {
	// Name is the name given to this test for the Constraint under test.
	Name string

	// Skipped is whether this Case was skipped while running its parent Test.
	Skipped bool

	// Error is the either:
	// 1) why this case failed, or
	// 2) the error which prevented running this case.
	// We don't need to distinguish between 1 and 2 - they are both treated as
	// failures.
	Error error

	// Runtime is the time it took for this Case to run.
	Runtime Duration

	// Trace is an explanation of the underlying constraint evaluation.
	// For instance, for OPA based evaluations, the trace is an explanation of the rego query:
	// https://www.openpolicyagent.org/docs/v0.44.0/policy-reference/#tracing
	// NOTE: This is a string pointer to differentiate between an empty ("") trace and an unset one (nil);
	// also for efficiency reasons as traces could be arbitrarily large theoretically.
	Trace *string
}

// IsFailure returns true if the test failed to execute or produced an
// unexpected result.
func (r *CaseResult) IsFailure() bool {
	return r.Error != nil
}
