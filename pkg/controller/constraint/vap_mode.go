package constraint

import (
	"errors"
	"flag"
	"fmt"
)

// VAPGenerationMode determines whether generated VAPs are shared by a template or specialized per Constraint.
type VAPGenerationMode string

const (
	// VAPGenerationModeTemplate preserves the shared parameterized VAP topology.
	VAPGenerationModeTemplate VAPGenerationMode = "template"
	// VAPGenerationModeConstraint creates one VAP with inlined parameters per Constraint.
	VAPGenerationModeConstraint VAPGenerationMode = "constraint"
	vapAPIVersionV1             string            = "v1"
	vapAPIVersionV1Beta1        string            = "v1beta1"
)

var (
	// ErrInvalidVAPGenerationMode indicates an unsupported VAP generation topology.
	ErrInvalidVAPGenerationMode = errors.New("invalid VAP generation mode")
	vapGenerationMode           = flag.String("vap-generation-mode", string(VAPGenerationModeTemplate), "(alpha) VAP generation topology. Allowed values are template, which creates one parameterized VAP per ConstraintTemplate, and constraint, which creates one VAP with inlined parameters per Constraint.")
)

// GetVAPGenerationMode returns the configured VAP generation topology.
func GetVAPGenerationMode() VAPGenerationMode {
	defaultFlagsMux.RLock()
	defer defaultFlagsMux.RUnlock()
	return VAPGenerationMode(*vapGenerationMode)
}

// SetVAPGenerationMode updates the VAP generation topology after validation.
func SetVAPGenerationMode(mode VAPGenerationMode) error {
	if err := validateVAPGenerationMode(mode); err != nil {
		return err
	}
	defaultFlagsMux.Lock()
	defer defaultFlagsMux.Unlock()
	*vapGenerationMode = string(mode)
	return nil
}

// ValidateVAPGenerationMode validates the configured VAP generation topology.
func ValidateVAPGenerationMode() error {
	return validateVAPGenerationMode(GetVAPGenerationMode())
}

func validateVAPGenerationMode(mode VAPGenerationMode) error {
	switch mode {
	case VAPGenerationModeTemplate, VAPGenerationModeConstraint:
		return nil
	default:
		return fmt.Errorf("%w %q: must be %q or %q", ErrInvalidVAPGenerationMode, mode, VAPGenerationModeTemplate, VAPGenerationModeConstraint)
	}
}
