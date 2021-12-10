package client

import (
	"fmt"
	"regexp"
	"sort"
)

type Opt func(*Client) error

// targetNameRegex defines allowable target names.
// Does not match empty string.
var targetNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9.]*$`)

// Targets defines the targets Client will pass review requests to.
func Targets(ts ...TargetHandler) Opt {
	return func(c *Client) error {
		handlers := make(map[string]TargetHandler, len(ts))

		invalid := validateTargetNames(ts)
		if len(invalid) > 0 {
			return fmt.Errorf("%w: target names %v are not of the form %q",
				ErrCreatingClient, invalid, targetNameRegex.String())
		}

		for _, t := range ts {
			handlers[t.GetName()] = t
		}
		c.targets = handlers

		return nil
	}
}

// validateTargetNames returns the invalid target names from the passed
// TargetHandlers.
func validateTargetNames(ts []TargetHandler) []string {
	var invalid []string

	for _, t := range ts {
		name := t.GetName()
		if !targetNameRegex.MatchString(name) {
			invalid = append(invalid, name)
		}
	}
	sort.Strings(invalid)

	return invalid
}

// AllowedDataFields sets the fields under `data` that Rego in ConstraintTemplates
// can access. If unset, all fields can be accessed. Only fields recognized by
// the system can be enabled.
func AllowedDataFields(fields ...string) Opt {
	return func(c *Client) error {
		c.allowedDataFields = fields
		return nil
	}
}
