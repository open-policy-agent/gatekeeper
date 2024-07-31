package constraints

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

type ScopedEnforcementAction struct {
	Action            string             `json:"action"`
	EnforcementPoints []EnforcementPoint `json:"enforcementPoints"`
}

type EnforcementAction string

type EnforcementPoint struct {
	Name string `json:"name"`
}

const (
	// Group is the API Group of Constraints.
	Group = "constraints.gatekeeper.sh"

	// AllEnforcementPoints is a wildcard to indicate all enforcement points.
	AllEnforcementPoints = "*"
)

const (
	// Deny indicates that if a review fails validation for a
	// Constraint, that it should be rejected. Errors encountered running
	// validation are treated as failing validation.
	//
	// This is the default EnforcementAction.
	Deny   EnforcementAction = "deny"
	Warn   EnforcementAction = "warn"
	Scoped EnforcementAction = "scoped"
)

var (
	// ErrInvalidConstraint is a generic error that a Constraint is invalid for
	// some reason.
	ErrInvalidConstraint = errors.New("invalid Constraint")

	// ErrSchema is a specific error that a Constraint failed schema validation.
	ErrSchema = errors.New("schema validation failed")

	// ErrMissingRequiredField is a specific error that a field is missing from a Constraint.
	ErrMissingRequiredField = errors.New("missing required field")

	ErrInvalidSpecEnforcementAction = errors.New("scopedEnforcementActions value must be a [{action: string, enforcementPoints: [{name: string}]}]")
)

// GetEnforcementAction returns a Constraint's enforcementAction, which indicates
// what should be done if a review violates a Constraint, or the Constraint fails
// to run.
//
// Returns an error if spec.enforcementAction is defined and is not a string.
func GetEnforcementAction(constraint *unstructured.Unstructured) (string, error) {
	action, found, err := unstructured.NestedString(constraint.Object, "spec", "enforcementAction")
	if err != nil {
		return "", fmt.Errorf("%w: invalid spec.enforcementAction", ErrInvalidConstraint)
	}

	if !found {
		return string(Deny), nil
	}

	return action, nil
}

func IsEnforcementActionScoped(action string) bool {
	return action == string(Scoped)
}

// GetEnforcementActionsForEP returns a map of enforcement actions for enforcement points passed in.
func GetEnforcementActionsForEP(constraint *unstructured.Unstructured, eps []string) (map[string][]string, error) {
	scopedActions, found, err := getNestedFieldAsArray(constraint.Object, "spec", "scopedEnforcementActions")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSpecEnforcementAction, err)
	}
	if !found {
		return nil, fmt.Errorf("%w: spec.scopedEnforcementActions must be defined", ErrMissingRequiredField)
	}

	scopedEnforcementActions, err := convertToSliceScopedEnforcementAction(scopedActions)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConstraint, err)
	}

	enforcementPointsToActionsMap := make(map[string]map[string]bool)
	for _, ep := range eps {
		enforcementPointsToActionsMap[ep] = make(map[string]bool)
	}
	for _, scopedEA := range scopedEnforcementActions {
		for _, enforcementPoint := range scopedEA.EnforcementPoints {
			epName := enforcementPoint.Name
			if epName == AllEnforcementPoints {
				for _, ep := range eps {
					enforcementPointsToActionsMap[ep][scopedEA.Action] = true
				}
				break
			}
			if _, ok := enforcementPointsToActionsMap[epName]; ok {
				enforcementPointsToActionsMap[epName][scopedEA.Action] = true
			}
		}
	}
	enforcementActionsForEPs := make(map[string][]string)
	for ep, actions := range enforcementPointsToActionsMap {
		if len(actions) == 0 {
			continue
		}
		enforcementActionsForEPs[ep] = make([]string, 0, len(actions))
		for action := range actions {
			enforcementActionsForEPs[ep] = append(enforcementActionsForEPs[ep], action)
		}
	}

	return enforcementActionsForEPs, nil
}

// Helper function to access nested fields as an array.
func getNestedFieldAsArray(obj map[string]interface{}, fields ...string) ([]interface{}, bool, error) {
	value, found, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	if arr, ok := value.([]interface{}); ok {
		return arr, true, nil
	}
	return nil, false, nil
}

// Helper function to convert a value to a []ScopedEnforcementAction.
func convertToSliceScopedEnforcementAction(value interface{}) ([]ScopedEnforcementAction, error) {
	var result []ScopedEnforcementAction
	arr, ok := value.([]interface{})
	if !ok {
		return nil, ErrInvalidSpecEnforcementAction
	}
	for _, v := range arr {
		m, ok := v.(map[string]interface{})
		if !ok {
			return nil, ErrInvalidSpecEnforcementAction
		}
		scopedEA := &ScopedEnforcementAction{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(m, scopedEA); err != nil {
			return nil, err
		}
		result = append(result, *scopedEA)
	}
	return result, nil
}
