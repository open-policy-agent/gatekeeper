package constraints

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// Group is the API Group of Constraints.
	Group = "constraints.gatekeeper.sh"

	// EnforcementActionDeny indicates that if a review fails validation for a
	// Constraint, that it should be rejected. Errors encountered running
	// validation are treated as failing validation.
	//
	// This is the default EnforcementAction.
	EnforcementActionDeny   = "deny"
	EnforcementActionScoped = "scoped"

	// WebhookEnforcementPoint is the enforcement point for admission.
	WebhookEnforcementPoint = "admission.k8s.io"

	// AuditEnforcementPoint is the enforcement point for audit.
	AuditEnforcementPoint = "audit.gatekeeper.sh"

	// GatorEnforcementPoint is the enforcement point for gator cli.
	GatorEnforcementPoint = "gator.gatekeeper.sh"
)

var (
	// ErrInvalidConstraint is a generic error that a Constraint is invalid for
	// some reason.
	ErrInvalidConstraint = errors.New("invalid Constraint")

	// ErrSchema is a specific error that a Constraint failed schema validation.
	ErrSchema = errors.New("schema validation failed")
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
	return action == EnforcementActionScoped
}

// GetEnforcementActionsForEP returns a map of enforcement actions for each enforcement point.
func GetEnforcementActionsForEP(constraint *unstructured.Unstructured, ep string) ([]string, error) {
	// Access the scopedEnforcementAction field
	scopedActions, found, err := getNestedFieldAsArray(constraint.Object, "spec", "scopedEnforcementActions")
	if err != nil {
		return nil, fmt.Errorf("%w: invalid spec.enforcementActionPerEP", ErrInvalidConstraint)
	}

	// Return early if scopedEnforcementAction is not found
	if !found {
		return nil, nil
	}

	// Convert scopedActions to a slice of map[string]interface{}
	scopedEnforcementActions, err := convertToMapSlice(scopedActions)
	if err != nil {
		return nil, fmt.Errorf("%w: spec.scopedEnforcementAction must be an array", ErrInvalidConstraint)
	}

	actions := make(map[string]bool)
	for _, scopedEnforcementAction := range scopedEnforcementActions {
		// Access the enforcementPoints field
		enforcementPoints, found, err := getNestedFieldAsArray(scopedEnforcementAction, "enforcementPoints")
		if err != nil || !found {
			continue
		}

		// Iterate over enforcementPoints
		for _, enforcementPoint := range enforcementPoints {
			// Convert enforcementPoint to map[string]string
			enforcementPointMap, err := convertToMapInterface(enforcementPoint)
			if err != nil {
				continue
			}

			if pt, ok := enforcementPointMap["name"].(string); ok {
				// Check if enforcementPoint matches the current ep or "*"
				if pt == ep || pt == "*" {
					// Access the action field
					action, found, err := getNestedFieldAsString(scopedEnforcementAction, "action")
					if err != nil || !found {
						continue
					}

					// Add action to the actionMap
					actions[action] = true
				}
			}
		}
	}
	actionsForEPs := []string{}
	for action := range actions {
		actionsForEPs = append(actionsForEPs, action)
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

// Helper function to access nested fields as a string.
func getNestedFieldAsString(obj map[string]interface{}, fields ...string) (string, bool, error) {
	for _, field := range fields {
		value, found, err := unstructured.NestedString(obj, field)
		if err != nil {
			return "", false, err
		}
		if found {
			return value, true, nil
		}
	}
	return "", false, nil
}

// Helper function to convert a value to a map[string]interface{}.
func convertToMapInterface(value interface{}) (map[string]interface{}, error) {
	if m, ok := value.(map[string]interface{}); ok {
		return m, nil
	}
	return nil, fmt.Errorf("value must be a map[string]interface{}")
}

// Helper function to convert a value to a []map[string]interface{}.
func convertToMapSlice(value interface{}) ([]map[string]interface{}, error) {
	if arr, ok := value.([]interface{}); ok {
		result := make([]map[string]interface{}, 0, len(arr))
		for _, v := range arr {
			if m, ok := v.(map[string]interface{}); ok {
				result = append(result, m)
			}
		}
		return result, nil
	}
	return nil, fmt.Errorf("value must be a []interface{}")
}
