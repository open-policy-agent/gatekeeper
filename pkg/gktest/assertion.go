package gktest

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/pkg/gktest/uint64bool"
)

// An Assertion is a declaration about the data returned by running an object
// against a Constraint.
type Assertion struct {
	// Violations, if set, indicates either whether there are violations, or how
	// many violations match this assertion.
	//
	// The value may be either an integer, of a boolean. If an integer, exactly
	// this number of violations must otherwise match this Assertion. If true,
	// at least one violation must match this Assertion. If false, zero violations
	// must match this Assertion.
	//
	// Defaults to true.
	Violations *uint64bool.Uint64OrBool `json:"violations,omitempty" yaml:"violations,omitempty"`

	// Message is a regular expression which matches the Msg field of individual
	// violations.
	//
	// If unset, has no effect.
	Message *string `json:"message,omitempty"`

	onceMsgRegex sync.Once
	msgRegex     *regexp.Regexp
}

func (a *Assertion) Run(results []*types.Result) error {
	matching := uint64(0)
	var messages []string

	for _, r := range results {
		messages = append(messages, r.Msg)

		matches, err := a.matches(r)
		if err != nil {
			return err
		}

		if matches {
			matching++
		}
	}

	if a.Violations == nil {
		a.Violations = uint64bool.FromBool(true)
	}

	switch a.Violations.Type {
	case uint64bool.Bool:
		if a.Violations.BoolVal && matching == 0 {
			return ErrUnexpectedNoViolations
		} else if !a.Violations.BoolVal && matching != 0 {
			if matching != 0 {
				return fmt.Errorf("%w: got messages %v",
					ErrUnexpectedViolation, messages)
			}
		}

		return nil
	case uint64bool.Uint64:
		wantMatching := a.Violations.Uint64Val

		if wantMatching == 0 && matching != 0 {
			return fmt.Errorf("%w: got messages %v",
				ErrUnexpectedViolation, messages)
		} else if wantMatching != matching {
			return fmt.Errorf("%w: got %d violations but want %d",
				ErrNumViolations, matching, wantMatching)
		}

		return nil
	default:
	}

	return nil
}

func (a *Assertion) matches(result *types.Result) (bool, error) {
	r, err := a.getMsgRegex()
	if err != nil {
		return false, err
	}

	if r != nil {
		return r.MatchString(result.Msg), nil
	}

	return true, nil
}

func (a *Assertion) getMsgRegex() (*regexp.Regexp, error) {
	if a.Message == nil {
		return nil, nil
	}

	var err error
	a.onceMsgRegex.Do(func() {
		a.msgRegex, err = regexp.Compile(*a.Message)
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRegex, err)
	}

	return a.msgRegex, nil
}
