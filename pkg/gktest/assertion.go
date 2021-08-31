package gktest

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
	// must be either "yes" or "no".
	//
	// Defaults to true.
	Violations *intstr.IntOrString `json:"violations,omitempty"`

	// Message is a regular expression which matches the Msg field of individual
	// violations.
	//
	// If unset, has no effect.
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

	switch a.Violations.Type {
	case intstr.String:
		switch a.Violations.StrVal {
		case "yes":
			if matching == 0 {
				return ErrUnexpectedNoViolations
			}
		case "no":
			if matching != 0 {
				return fmt.Errorf("%w: got messages %v",
					ErrUnexpectedViolation, messages)
			}
		default:
			return fmt.Errorf(`%w: assertion.violation, if set, must be an integer, "yes", or "no"`, ErrInvalidYAML)
		}

		return nil
	case intstr.Int:
		wantMatching := a.Violations.IntVal

		if wantMatching == 0 {
			if matching != 0 {
				return fmt.Errorf("%w: got messages %v",
					ErrUnexpectedViolation, messages)
			}

			return nil
		}

		if matching == 0 {
			return fmt.Errorf("%w: want %d", ErrUnexpectedNoViolations, wantMatching)
		} else if matching != wantMatching {
			return fmt.Errorf("%w: got %d violations but want %d",
				ErrNumViolations, matching, wantMatching)
		}

		return nil
	default:
		// Requires a bug in intstr unmarshalling code, or a misuse of the IntOrStr
		// type in Go code.
		return fmt.Errorf("%w: assertion.violations improperly parsed to type %d",
			ErrInvalidYAML, a.Violations.Type)
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
