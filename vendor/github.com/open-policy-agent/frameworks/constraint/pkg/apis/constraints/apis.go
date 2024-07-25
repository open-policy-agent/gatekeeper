package constraints

import (
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

type ScopedEnforcementAction struct {
	Action            string             `json:"action"`
	EnforcementPoints []EnforcementPoint `json:"enforcementPoints"`
}

type EnforcementPoint struct {
	Name string `json:"name"`
}

const (
	// Group is the API Group of Constraints.
	Group = "constraints.gatekeeper.sh"

	// EnforcementActionDeny indicates that if a review fails validation for a
	// Constraint, that it should be rejected. Errors encountered running
	// validation are treated as failing validation.
	//
	// This is the default EnforcementAction.
	EnforcementActionDeny = "deny"

	EnforcementActionScoped = "scoped"

	// AllEnforcementPoints is a wildcard to indicate all enforcement points.
	AllEnforcementPoints = "*"
)

var (
	// ErrInvalidConstraint is a generic error that a Constraint is invalid for
	// some reason.
	ErrInvalidConstraint = errors.New("invalid Constraint")

	// ErrSchema is a specific error that a Constraint failed schema validation.
	ErrSchema = errors.New("schema validation failed")

	// ErrMissingRequiredField is a specific error that a field is missing from a Constraint.
	ErrMissingRequiredField = errors.New("missing required field")
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
		return EnforcementActionDeny, nil
	}

	return action, nil
}

func IsEnforcementActionScoped(action string) bool {
	return strings.EqualFold(action, EnforcementActionScoped)
}

// GetEnforcementActionsForEP returns a map of enforcement actions for enforcement points passed in.
func GetEnforcementActionsForEP(constraint *unstructured.Unstructured, eps []string) (map[string]map[string]bool, error) {
	if len(eps) == 0 {
		return nil, fmt.Errorf("enforcement points must be provided to get enforcement actions")
	}

	scopedActions, found, err := getNestedFieldAsArray(constraint.Object, "spec", "scopedEnforcementActions")
	if err != nil {
		return nil, fmt.Errorf("%w: invalid spec.enforcementActionPerEP", ErrInvalidConstraint)
	}
	if !found {
		return nil, fmt.Errorf("%w: spec.scopedEnforcementAction must be defined", ErrMissingRequiredField)
	}

	scopedEnforcementActions, err := convertToSliceScopedEnforcementAction(scopedActions)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConstraint, err)
	}

	// Flag to indicate if all enforcement points should be enforced
	enforceAll := false
	// Initialize a map to hold enforcement actions for each enforcement point
	actionsForEPs := make(map[string]map[string]bool)
	// Populate the actionsForEPs map with enforcement points from eps, initializing their action maps
	for _, enforcementPoint := range eps {
		if enforcementPoint == AllEnforcementPoints {
			enforceAll = true // Set enforceAll to true if the special identifier for all enforcement points is found
		}
		actionsForEPs[enforcementPoint] = make(map[string]bool) // Initialize the action map for the enforcement point
	}

	// Iterate over the scoped enforcement actions to populate actions for each enforcement point
	for _, scopedEA := range scopedEnforcementActions {
		for _, enforcementPoint := range scopedEA.EnforcementPoints {
			epName := strings.ToLower(enforcementPoint.Name)
			ea := strings.ToLower(scopedEA.Action)
			// If enforceAll is true, or the enforcement point is explicitly listed, initialize its action map
			if _, ok := actionsForEPs[epName]; !ok && enforceAll {
				actionsForEPs[epName] = make(map[string]bool)
			}
			// Skip adding actions for enforcement points not in the list unless enforceAll is true
			if _, ok := actionsForEPs[epName]; !ok && epName != AllEnforcementPoints {
				continue
			}
			// If the enforcement point is the special identifier for all, apply the action to all enforcement points
			switch epName {
			case AllEnforcementPoints:
				for ep := range actionsForEPs {
					actionsForEPs[ep][ea] = true
				}
			default:
				actionsForEPs[epName][ea] = true
			}
		}
	}

	return actionsForEPs, nil
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
	if arr, ok := value.([]interface{}); ok {
		for _, v := range arr {
			if m, ok := v.(map[string]interface{}); ok {
				scopedEA := &ScopedEnforcementAction{}
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(m, scopedEA); err != nil {
					return nil, err
				}
				result = append(result, *scopedEA)
			} else {
				return nil, fmt.Errorf("scopedEnforcementActions value must be a []scopedEnforcementAction{action: string, enforcementPoints: []EnforcementPoint{name: string}}")
			}
		}
		return result, nil
	}
	return nil, fmt.Errorf("scopedEnforcementActions value must be a []scopedEnforcementAction{action: string, enforcementPoints: []EnforcementPoint{name: string}}")
}
