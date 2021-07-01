package gktest

import (
	"errors"
)

// Result is the structured form of the results of running a test.
type Result interface {
	// String formats the Result for printing.
	String() string
	// IsFailure returns true if either a test failed or there was a problem
	// executing tests.
	IsFailure() bool

	Equal(Result) bool
}

type Success struct{}

var _ Result = &Success{}

func (s *Success) String() string {
	panic("implement me")
}

func (s *Success) IsFailure() bool {
	panic("implement me")
}

func (s *Success) Equal(right Result) bool {
	success, isSuccess := right.(*Success)
	if !isSuccess {
		return false
	}
	return *s == *success
}

func errorResult(err error) *ErrorResult {
	return &ErrorResult{err}
}

type ErrorResult struct {
	error
}

func (r *ErrorResult) Equal(right Result) bool {
	err, isErr := right.(*ErrorResult)
	if !isErr {
		return false
	}

	// errors.Is is not commutative, but Equal should be.
	return errors.Is(r.error, err.error) || errors.Is(err.error, r.error)
}

func (r *ErrorResult) String() string {
	return ""
}

func (r *ErrorResult) IsFailure() bool {
	return true
}
