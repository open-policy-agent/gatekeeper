package util

import "testing"

func TestValidateEnforcementAction(t *testing.T) {
	err := ValidateEnforcementAction("")
	if err == nil {
		t.Errorf("ValidateEnforcementAction should error when enforcementAction is not recognized, %v", err)
	}

	err = ValidateEnforcementAction("notsupported")
	if err == nil {
		t.Errorf("ValidateEnforcementAction should error when enforcementAction is not recognized, %v", err)
	}

	err = ValidateEnforcementAction("dryrun")
	if err != nil {
		t.Errorf("ValidateEnforcementAction should not error when enforcementAction is recognized, %v", err)
	}
}
