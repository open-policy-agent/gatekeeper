package client

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
	"github.com/open-policy-agent/frameworks/constraint/pkg/handler"
)

type Opt func(*Client) error

// targetNameRegex defines allowable target names.
// Does not match empty string.
var targetNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9.]*$`)

// Targets defines the targets Client will pass review requests to.
func Targets(ts ...handler.TargetHandler) Opt {
	return func(c *Client) error {
		handlers := make(map[string]handler.TargetHandler, len(ts))

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
func validateTargetNames(ts []handler.TargetHandler) []string {
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

// Driver defines the Rego execution environment.
func Driver(d drivers.Driver) Opt {
	return func(client *Client) error {
		if d.Name() == "" {
			return ErrNoDriverName
		}
		if _, ok := client.drivers[d.Name()]; ok {
			return fmt.Errorf("%w: %s", ErrDuplicateDriver, d.Name())
		}
		client.drivers[d.Name()] = d
		client.driverPriority[d.Name()] = len(client.drivers)
		return nil
	}
}

func IgnoreNoReferentialDriverWarning(ignore bool) Opt {
	return func(client *Client) error {
		client.ignoreNoReferentialDriverWarning = ignore
		return nil
	}
}

func EnforcementPoints(eps ...string) Opt {
	return func(client *Client) error {
		client.enforcementPoints = eps
		return nil
	}
}
