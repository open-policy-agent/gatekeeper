package client

import (
	"fmt"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
)

// NewClient creates a new client.
func NewClient(opts ...Opt) (*Client, error) {
	c := &Client{
		templates:      make(map[string]*templateClient),
		drivers:        make(map[string]drivers.Driver),
		driverPriority: make(map[string]int),
	}

	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}

	if len(c.targets) == 0 {
		return nil, fmt.Errorf("%w: must specify at least one target with client.Targets",
			ErrCreatingClient)
	}

	if len(c.enforcementPoints) == 0 {
		return nil, fmt.Errorf("%w: must specify at least one enforcement point with client.EnforcementPoints",
			ErrCreatingClient)
	}

	return c, nil
}
