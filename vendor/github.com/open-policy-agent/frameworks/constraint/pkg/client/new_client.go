package client

import (
	"fmt"
)

// NewClient creates a new client.
func NewClient(opts ...Opt) (*Client, error) {
	c := &Client{
		templates: make(map[string]*templateClient),
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

	return c, nil
}
