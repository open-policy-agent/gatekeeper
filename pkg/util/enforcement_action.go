package util

import "fmt"

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
