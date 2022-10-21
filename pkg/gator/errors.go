package gator

import "errors"

var (
	// ErrNotATemplate indicates the user-indicated file does not contain a
	// ConstraintTemplate.
	ErrNotATemplate = errors.New("not a ConstraintTemplate")
	// ErrNotAConstraint indicates the user-indicated file does not contain a
	// Constraint.
	ErrNotAConstraint = errors.New("not a Constraint")
	// ErrAddingTemplate indicates a problem instantiating a Suite's ConstraintTemplate.
	ErrAddingTemplate = errors.New("adding template")
	// ErrAddingConstraint indicates a problem instantiating a Suite's Constraint.
	ErrAddingConstraint = errors.New("adding constraint")
	// ErrInvalidSuite indicates a Suite does not define the required fields.
	ErrInvalidSuite = errors.New("invalid Suite")
	// ErrCreatingClient indicates an error instantiating the Client which compiles
	// Constraints and runs validation.
	ErrCreatingClient = errors.New("creating client")
	// ErrInvalidCase indicates a Case cannot be run due to not being configured properly.
	ErrInvalidCase = errors.New("invalid Case")
	// ErrNumViolations indicates an Object did not get the expected number of
	// violations.
	ErrNumViolations = errors.New("unexpected number of violations")
	// ErrInvalidRegex indicates a Case specified a Violation regex that could not
	// be compiled.
	ErrInvalidRegex = errors.New("message contains invalid regular expression")
	// ErrInvalidFilter indicates that Filter construction failed.
	ErrInvalidFilter = errors.New("invalid test filter")
	// ErrNoObjects indicates that a specified YAML file contained no objects.
	ErrNoObjects = errors.New("missing objects")
	// ErrMultipleObjects indicates that a specified YAML file contained multiple objects.
	ErrMultipleObjects = errors.New("object file must contain exactly one object")
	// ErrAddInventory indicates that an object that was declared to be part of
	// data.inventory was unable to be added.
	ErrAddInventory = errors.New("unable to add object to data.inventory")
	// ErrConvertingTemplate means we were able to parse a template, but not convert
	// it into the version-independent format.
	ErrConvertingTemplate = errors.New("unable to convert template")
	// ErrValidConstraint occurs when a test's configuration signals an expectation
	// that a constraint should fail validation but no validation error is raised.
	ErrValidConstraint = errors.New("constraint should have failed schema validation")
	// ErrInvalidK8sAdmissionReview occurs when a test attempts to pass in an AdmissionReview
	// object but we fail to convert the unstructured object into a typed AdmissionReview one.
	ErrInvalidK8sAdmissionReview = errors.New("not an AdmissionReview object")
	// ErrMissingK8sAdmissionRequest occurs when a test attempts to pass in an AdmissionReview
	// object but it does not actually pass in an AdmissionRequest object.
	ErrMissingK8sAdmissionRequest = errors.New("missing an AdmissionRequest object")
	// ErrReviewObject occurs when a test attempts to pass in an AdmissionRequest with no
	// object or oldObject for the underlying framework to review.
	// This mimicks the k8s api server behvaior.
	ErrNoObjectForReview = errors.New("no object or oldObject found to review")
)
