package gktest

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Suite defines a set of Constraint tests.
type Suite struct {
	metav1.ObjectMeta

	// Tests is a list of Template&Constraint pairs, with tests to run on
	// each.
	Tests []Test `json:"spec"`
}

// Test defines a Template&Constraint pair to instantiate, and Cases to
// run on the instantiated Constraint.
type Test struct {
	Name string `json:"name"`

	// Template is the path to the ConstraintTemplate, relative to the file
	// defining the Suite.
	Template string `json:"template"`

	// Constraint is the path to the Constraint, relative to the file defining
	// the Suite. Must be an instance of Template.
	Constraint string `json:"constraint"`

	// Cases are the test cases to run on the instantiated Constraint.
	Cases []Case `json:"cases"`
}

// Case runs Constraint against a YAML object.
type Case struct {
	Name string `json:"name"`

	// Object is the path to the file containing a Kubernetes object to test.
	Object string `json:"object"`

	// If unset, Constraint should return no violations. An empty Violations
	// object represents that the Constraint is expected to deny Object, but no
	// specific validation of the rejection.
	Violations *Violations `json:"assertions,omitempty"`
}

// Violations indicate Object should be rejected by the Constraint, and the
// specifics.
type Violations struct {
	// Count is the number of distinct violations the Constraint should return for
	// Object.
	Count *int32 `json:"count,omitempty"`

	// Messages is a list of exact-string matches the Case expects the Constraint
	// will return. Each message must match at least one violation's message, and
	// multiple messages may match a single violation.
	Messages []string `json:"messages,omitempty"`
}
