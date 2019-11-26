package util

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type EnforcementAction string

const (
	Deny         EnforcementAction = "deny"
	Dryrun       EnforcementAction = "dryrun"
	Unrecognized EnforcementAction = "unrecognized"
)

var supportedEnforcementActions = []EnforcementAction{Deny, Dryrun}
var KnownEnforcementActions = []EnforcementAction{Deny, Dryrun, Unrecognized}

func ValidateEnforcementAction(input EnforcementAction) error {
	for _, n := range supportedEnforcementActions {
		if input == n {
			return nil
		}
	}
	return fmt.Errorf("Could not find the provided enforcementAction value within the supported list %v", supportedEnforcementActions)
}

func GetEnforcementAction(item map[string]interface{}) (EnforcementAction, error) {
	enforcementActionSpec, _, err := unstructured.NestedString(item, "spec", "enforcementAction")
	if err != nil {
		return "", err
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
