package gktest

// SuiteResult is the Result of running a Suite of tests.
type SuiteResult struct {
	// Path is the absolute path to the file which defines Suite.
	Path string

	// Error is the error which stopped the Suite from executing.
	// If defined, TestResults is empty.
	Error error

	// TestResults are the results of running the tests for each defined
	// Template/Constraint pair.
	TestResults []TestResult
}

// TestResult is the results of:
// 1) Compiling the ConstraintTemplate,
// 2) Instantiating the Constraint, and
// 3) Running all Tests defined for the Constraint.
type TestResult struct {
	// Name is the name given to the Template/Constraint pair under test.
	Name string

	// Error is the error which prevented running tests for this Constraint.
	// If defined, CaseResults is empty.
	Error error

	// CaseResults are individual results for all tests defined for this Constraint.
	CaseResults []CaseResult
}

// CaseResult is the result of evaluating a Constraint against a kubernetes
// object, and comparing the result with the expected result.
type CaseResult struct {
	// Name is the name given to this test for the Constraint under test.
	Name string

	// Error is the either:
	// 1) why this test failed, or
	// 2) the error which prevented running this test.
	// We don't need to distinguish between 1 and 2 - they are both treated as
	// test failures.
	Error error
}
