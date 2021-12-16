package gator

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// An Assertion is a declaration about the data returned by running an object
// against a Constraint.
type Assertion struct {
	// Violations, if set, indicates either whether there are violations, or how
	// many violations match this assertion.
	//
	// The value may be either an integer, of a string. If an integer, exactly
	// this number of violations must otherwise match this Assertion. If a string,
	// must be either "yes" or "no". If "yes" at least one violation must match
	// the Assertion to be satisfied. If "no", there must be zero violations
	// matching the Assertion to be satisfied.
	//
	// Defaults to "yes".
	Violations *intstr.IntOrString `json:"violations,omitempty"`

	// Message is a regular expression which matches the Msg field of individual
	// violations.
	//
	// If unset, has no effect and all violations match this Assertion.
	Message *string `json:"message,omitempty"`

	onceMsgRegex sync.Once
	msgRegex     *regexp.Regexp
}

func (a *Assertion) Run(results []*types.Result) error {
	matching := int32(0)
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

	// Default to assuming the object fails validation.
	if a.Violations == nil {
		a.Violations = intStrFromStr("yes")
	}

	err := a.matchesCount(matching)
	if err != nil {
		return fmt.Errorf("%w: got messages %v", err, messages)
	}

	return nil
}

func (a *Assertion) matchesCount(matching int32) error {
	switch a.Violations.Type {
	case intstr.Int:
		if a.Violations.IntVal < 0 {
			return fmt.Errorf(`%w: assertion.violation, if set, must be a nonnegative integer, "yes", or "no"`,
				ErrInvalidYAML)
		}
		return a.matchesCountInt(matching)
	case intstr.String:
		return a.matchesCountStr(matching)
	default:
		// Requires a bug in intstr unmarshalling code, or a misuse of the IntOrStr
		// type in Go code.
		return fmt.Errorf("%w: assertion.violations improperly parsed to type %d",
			ErrInvalidYAML, a.Violations.Type)
	}
}

func (a *Assertion) matchesCountInt(matching int32) error {
	wantMatching := a.Violations.IntVal
	if wantMatching != matching {
		if a.Message != nil {
			return fmt.Errorf("%w: got %d violations containing %q but want exactly %d",
				ErrNumViolations, matching, *a.Message, wantMatching)
		}
		return fmt.Errorf("%w: got %d violations but want exactly %d",
			ErrNumViolations, matching, wantMatching)
	}

	return nil
}

func (a *Assertion) matchesCountStr(matching int32) error {
	switch a.Violations.StrVal {
	case "yes":
		if matching == 0 {
			if a.Message != nil {
				return fmt.Errorf("%w: got %d violations containing %q but want at least %d",
					ErrNumViolations, matching, *a.Message, 1)
			}
			return fmt.Errorf("%w: got %d violations but want at least %d",
				ErrNumViolations, matching, 1)
		}

		return nil
	case "no":
		if matching > 0 {
			if a.Message != nil {
				return fmt.Errorf("%w: got %d violations containing %q but want none",
					ErrNumViolations, matching, *a.Message)
			}
			return fmt.Errorf("%w: got %d violations but want none",
				ErrNumViolations, matching)
		}

		return nil
	default:
		return fmt.Errorf(`%w: assertion.violation, if set, must be a nonnegative integer, "yes", or "no"`,
			ErrInvalidYAML)
	}
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
