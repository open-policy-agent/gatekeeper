package gator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Suite defines a set of Constraint tests.
type Suite struct {
	metav1.ObjectMeta

	// Tests is a list of Template&Constraint pairs, with tests to run on
	// each.
	Tests []Test `json:"tests"`

	// Path is the filepath of this Suite on disk.
	Path string `json:"-"`

	// Skip, if true, skips this Suite.
	Skip bool `json:"skip"`
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
	// Mutually exclusive with Invalid.
	Cases []*Case `json:"cases,omitempty"`

	// Invalid, if true, specifies that the Constraint is expected to be invalid
	// for the Template. For example - a required field is missing or is of the
	// wrong type.
	// Mutually exclusive with Cases.
	Invalid bool `json:"invalid"`

	// Skip, if true, skips this Test.
	Skip bool `json:"skip"`
}

// Case runs Constraint against a YAML object.
type Case struct {
	Name string `json:"name"`

	// Object is the path to the file containing a Kubernetes object to test.
	Object string `json:"object"`

	// Inventory is a list of paths to files containing Kubernetes objects to put
	// in data.inventory for testing referential constraints.
	Inventory []string `json:"inventory"`

	// Assertions are statements which must be true about the result of running
	// Review with the Test's Constraint on the Case's Object.
	//
	// All Assertions must succeed in order for the test to pass.
	// If no assertions are present, assumes reviewing Object produces no
	// violations.
	Assertions []Assertion `json:"assertions"`

	// Skip, if true, skips this Case.
	Skip bool `json:"skip"`
}
