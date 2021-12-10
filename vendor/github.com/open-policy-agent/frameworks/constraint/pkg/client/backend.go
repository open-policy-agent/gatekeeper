package client

import (
	"fmt"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type Backend struct {
	driver    drivers.Driver
	crd       *crdHelper
	hasClient bool
}

type BackendOpt func(*Backend)

func Driver(d drivers.Driver) BackendOpt {
	return func(b *Backend) {
		b.driver = d
	}
}

// NewBackend creates a new backend. A backend could be a connection to a remote
// server or a new local OPA instance.
//
// A BackendOpt setting driver, such as Driver() must be passed.
func NewBackend(opts ...BackendOpt) (*Backend, error) {
	helper, err := newCRDHelper()
	if err != nil {
		return nil, err
	}
	b := &Backend{crd: helper}
	for _, opt := range opts {
		opt(b)
	}

	if b.driver == nil {
		return nil, fmt.Errorf("%w: no driver supplied", ErrCreatingBackend)
	}

	return b, nil
}

// NewClient creates a new client for the supplied backend.
func (b *Backend) NewClient(opts ...Opt) (*Client, error) {
	if b.hasClient {
		return nil, fmt.Errorf("%w: only one client per backend is allowed",
			ErrCreatingClient)
	}

	var fields []string
	for k := range validDataFields {
		fields = append(fields, k)
	}

	c := &Client{
		backend:           b,
		constraints:       make(map[schema.GroupKind]map[string]*unstructured.Unstructured),
		templates:         make(map[templateKey]*templateEntry),
		allowedDataFields: fields,
	}

	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}

	for _, field := range c.allowedDataFields {
		if !validDataFields[field] {
			return nil, fmt.Errorf("%w: invalid data field %q; allowed fields are: %v",
				ErrCreatingClient, field, validDataFields)
		}
	}

	if len(c.targets) == 0 {
		return nil, fmt.Errorf("%w: must specify at least one target with client.Targets",
			ErrCreatingClient)
	}

	if err := b.driver.Init(); err != nil {
		return nil, err
	}

	if err := c.init(); err != nil {
		return nil, err
	}

	b.hasClient = true
	return c, nil
}
