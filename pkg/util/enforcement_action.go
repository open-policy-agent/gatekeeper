package util

import (
	"encoding/json"
	"errors"
	"fmt"

	apiconstraints "github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// EnforcementAction is the response we take to violations.
type EnforcementAction string

// The set of possible responses to policy violations.
const (
	Deny         EnforcementAction = "deny"
	Dryrun       EnforcementAction = "dryrun"
	Warn         EnforcementAction = "warn"
	Scoped       EnforcementAction = "scoped"
	Unrecognized EnforcementAction = "unrecognized"
)

const (
	// WebhookEnforcementPoint is the enforcement point for admission.
	WebhookEnforcementPoint = "validation.gatekeeper.sh"

	// AuditEnforcementPoint is the enforcement point for audit.
	AuditEnforcementPoint = "audit.gatekeeper.sh"

	// GatorEnforcementPoint is the enforcement point for gator cli.
	GatorEnforcementPoint = "gator.gatekeeper.sh"

	// VAP enforcement point for ValidatingAdmissionPolicy.
	VAPEnforcementPoint = "vap.k8s.io"

	// AllEnforcementPoints indicates all enforcement points.
	AllEnforcementPoints = "*"
)

var supportedEnforcementPoints = []string{WebhookEnforcementPoint, AuditEnforcementPoint, GatorEnforcementPoint, VAPEnforcementPoint}

var supportedEnforcementActions = []EnforcementAction{Deny, Dryrun, Warn, Scoped}

var supportedScopedActions = []EnforcementAction{Deny, Dryrun, Warn}

// KnownEnforcementActions are all defined EnforcementActions.
var KnownEnforcementActions = []EnforcementAction{Deny, Dryrun, Warn, Scoped, Unrecognized}

// ErrEnforcementAction indicates the passed EnforcementAction is not valid.
var ErrEnforcementAction = errors.New("unrecognized enforcementAction")

// ErrInvalidSpecEnforcementAction indicates that we were unable to parse the
// spec.enforcementAction field as it was not a string.
var ErrInvalidSpecEnforcementAction = errors.New("spec.enforcementAction must be a string")

var ErrUnrecognizedEnforcementPoint = errors.New("unrecognized enforcement points")

var ErrInvalidSpecScopedEnforcementAction = errors.New("spec.scopedEnforcementAction must be in the format of []{action: string, enforcementPoints: []{name: string}}")

func ValidateEnforcementAction(input EnforcementAction, item map[string]interface{}) error {
	switch input {
	case Scoped:
		return ValidateScopedEnforcementAction(item)
	case Dryrun, Deny, Warn:
		return nil
	default:
		return fmt.Errorf("%w: %q is not within the supported list %v",
			ErrEnforcementAction, input, supportedEnforcementActions)
	}
}

func ValidateScopedEnforcementAction(item map[string]interface{}) error {
	obj, err := GetScopedEnforcementAction(item)
	if err != nil {
		return fmt.Errorf("error fetching scopedEnforcementActions: %w", err)
	}

	var unrecognizedEnforcementPoints []string
	var unrecognizedEnforcementActions []string
	var errs []error
	// validating scopedEnforcementActions
	for _, scopedEnforcementAction := range *obj {
		switch EnforcementAction(scopedEnforcementAction.Action) {
		case Dryrun, Deny, Warn:
		default:
			unrecognizedEnforcementActions = append(unrecognizedEnforcementActions, scopedEnforcementAction.Action)
		}
		if len(scopedEnforcementAction.EnforcementPoints) == 0 {
			unrecognizedEnforcementPoints = append(unrecognizedEnforcementPoints, "")
		}
		for _, enforcementPoint := range scopedEnforcementAction.EnforcementPoints {
			switch enforcementPoint.Name {
			case WebhookEnforcementPoint, AuditEnforcementPoint, GatorEnforcementPoint, VAPEnforcementPoint, AllEnforcementPoints:
			default:
				unrecognizedEnforcementPoints = append(unrecognizedEnforcementPoints, enforcementPoint.Name)
			}
		}
	}
	if len(unrecognizedEnforcementPoints) > 0 {
		errs = append(errs, fmt.Errorf("%w: constraint will not be enforced for enforcement points %v, supported enforcement points are %v", ErrUnrecognizedEnforcementPoint, unrecognizedEnforcementPoints, supportedEnforcementPoints))
	}
	if len(unrecognizedEnforcementActions) > 0 {
		errs = append(errs, fmt.Errorf("%w: %v is not within the supported list %v", ErrEnforcementAction, unrecognizedEnforcementActions, supportedScopedActions))
	}
	return errors.Join(errs...)
}

func GetScopedEnforcementAction(item map[string]interface{}) (*[]apiconstraints.ScopedEnforcementAction, error) {
	scopedEnforcementActions, found, err := unstructured.NestedFieldNoCopy(item, "spec", "scopedEnforcementActions")
	if err != nil {
		return nil, fmt.Errorf("error fetching scopedEnforcementActions: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("scopedEnforcementActions is required")
	}
	return convertToScopedEnforcementActions(scopedEnforcementActions)
}

func convertToScopedEnforcementActions(object interface{}) (*[]apiconstraints.ScopedEnforcementAction, error) {
	j, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("could not convert unknown object to JSON: %w", err)
	}
	obj := []apiconstraints.ScopedEnforcementAction{}
	if err := json.Unmarshal(j, &obj); err != nil {
		return nil, fmt.Errorf("Could not convert JSON to scopedEnforcementActions: %w", err)
	}
	return &obj, nil
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
	// validating enforcement action - if it is not deny or dryrun or scoped, we are classifying as unrecognized
	switch enforcementAction {
	case Dryrun, Deny, Warn, Scoped:
		return enforcementAction, nil
	default:
		enforcementAction = Unrecognized
	}

	return enforcementAction, nil
}

func ScopedActionForEP(enforcementPoint string, u *unstructured.Unstructured) ([]string, error) {
	enforcementActions := []string{}
	scopedEnforcementActions, err := GetScopedEnforcementAction(u.Object)
	if err != nil {
		return nil, err
	}
	for _, scopedEnforcementAction := range *scopedEnforcementActions {
		if enforcementPointEnabled(scopedEnforcementAction, enforcementPoint) {
			enforcementActions = append(enforcementActions, scopedEnforcementAction.Action)
		}
	}
	return enforcementActions, nil
}

func enforcementPointEnabled(scopedEnforcementAction apiconstraints.ScopedEnforcementAction, enforcementPoint string) bool {
	for _, ep := range scopedEnforcementAction.EnforcementPoints {
		if ep.Name == enforcementPoint || ep.Name == AllEnforcementPoints {
			return true
		}
	}
	return false
}
