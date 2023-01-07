package audit

import (
	"testing"

	"github.com/pkg/errors"
)

func Test_mergeErrors(t *testing.T) {
	// one error
	errs := []error{errors.New("error 1")}
	expected := "error 1"
	result := mergeErrors(errs)
	if result == nil || result.Error() != expected {
		t.Errorf("Unexpected result for errs = %v: got %v, want %v", errs, result, expected)
	}

	// empty errors
	errs = []error{}
	expected = ""
	result = mergeErrors(errs)
	if result.Error() != expected {
		t.Errorf("Unexpected result for errs = %v: got %v, want %v", errs, result, expected)
	}

	// 3 errors
	errs = []error{errors.New("error 1"), errors.New("error 2"), errors.New("error 3")}
	expected = "error 1\nerror 2\nerror 3"
	result = mergeErrors(errs)
	if result == nil || result.Error() != expected {
		t.Errorf("Unexpected result for errs = %v: got %v, want %v", errs, result, expected)
	}

	// 2 errors with newlines
	errs = []error{errors.New("error 1\nerror 1.1"), errors.New("error 2\nerror 2.2")}
	expected = "error 1\nerror 1.1\nerror 2\nerror 2.2"
	result = mergeErrors(errs)
	if result == nil || result.Error() != expected {
		t.Errorf("Unexpected result for errs = %v: got %v, want %v", errs, result, expected)
	}
}
