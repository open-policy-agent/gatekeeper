package util

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var supportedEnforcementActions = []string{
	"deny",
	"dryrun",
}

func ValidateEnforcementAction(input string) error {
	for _, n := range supportedEnforcementActions {
		if input == n {
			return nil
		}
	}
	return fmt.Errorf("Could not find the provided enforcementAction value within the supported list %v", supportedEnforcementActions)
}

func GetEnforcementAction(item map[string]interface{}) (string, error) {
	enforcementAction, _, err := unstructured.NestedString(item, "spec", "enforcementAction")
	if err != nil {
		return "", err
	}
	// default enforcementAction is deny
	if enforcementAction == "" {
		enforcementAction = "deny"
	}
	// validating enforcement action - if it is not deny or dryrun, we are classifying as unrecognized
	if err := ValidateEnforcementAction(enforcementAction); err != nil {
		enforcementAction = "unrecognized"
	}

	return enforcementAction, nil
}
