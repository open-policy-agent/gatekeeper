package util

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// EnforcementAction is the response we take to violations.
type EnforcementAction string

// The set of possible responses to policy violations.
const (
	Deny         EnforcementAction = "deny"
	Dryrun       EnforcementAction = "dryrun"
	Warn         EnforcementAction = "warn"
	Unrecognized EnforcementAction = "unrecognized"
)

var supportedEnforcementActions = []EnforcementAction{Deny, Dryrun, Warn}

// KnownEnforcementActions are all defined EnforcementActions.
var KnownEnforcementActions = []EnforcementAction{Deny, Dryrun, Warn, Unrecognized}

// ErrEnforcementAction indicates the passed EnforcementAction is not valid.
var ErrEnforcementAction = errors.New("unrecognized enforcementAction")

// ErrInvalidSpecEnforcementAction indicates that we were unable to parse the
// spec.enforcementAction field as it was not a string.
var ErrInvalidSpecEnforcementAction = errors.New("spec.enforcementAction must be a string")

func ValidateEnforcementAction(input EnforcementAction) error {
	for _, n := range supportedEnforcementActions {
		if input == n {
			return nil
		}
	}
	return fmt.Errorf("%w: %q is not within the supported list %v",
		ErrEnforcementAction, input, supportedEnforcementActions)
}

func GetEnforcementAction(item map[string]interface{}) (EnforcementAction, error) {
	enforcementActionSpec, _, err := unstructured.NestedString(item, "spec", "enforcementAction")
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidSpecEnforcementAction, err)
	}
	enforcementAction := EnforcementAction(enforcementActionSpec)
	// default enforcementAction is deny
	if enforcementAction == "" {
		enforcementAction = Deny
	}
	// validating enforcement action - if it is not deny or dryrun, we are classifying as unrecognized
	if err := ValidateEnforcementAction(enforcementAction); err != nil {
		enforcementAction = Unrecognized
	}

	return enforcementAction, nil
}
