package client

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// NewClient creates a new client.
func NewClient(opts ...Opt) (*Client, error) {
	var fields []string
	for k := range validDataFields {
		fields = append(fields, k)
	}

	c := &Client{
		constraints:       make(map[schema.GroupKind]map[string]*unstructured.Unstructured),
		templates:         make(map[templateKey]*templateEntry),
		AllowedDataFields: fields,
	}

	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}

	for _, field := range c.AllowedDataFields {
		if !validDataFields[field] {
			return nil, fmt.Errorf("%w: invalid data field %q; allowed fields are: %v",
				ErrCreatingClient, field, validDataFields)
		}
	}

	if len(c.targets) == 0 {
		return nil, fmt.Errorf("%w: must specify at least one target with client.Targets",
			ErrCreatingClient)
	}

	if err := c.driver.Init(); err != nil {
		return nil, err
	}

	if err := c.init(); err != nil {
		return nil, err
	}

	return c, nil
}
