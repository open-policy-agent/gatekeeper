package client

import (
	"context"
	"errors"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
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

// NewBackend creates a new backend. A backend could be a connection to a remote server or
// a new local OPA instance.
func NewBackend(opts ...BackendOpt) (*Backend, error) {
	b := &Backend{crd: newCRDHelper()}
	for _, opt := range opts {
		opt(b)
	}

	if b.driver == nil {
		return nil, errors.New("No driver supplied to the backend")
	}

	return b, nil
}

// NewClient creates a new client for the supplied backend
func (b *Backend) NewClient(opts ...ClientOpt) (Client, error) {
	if b.hasClient {
		return nil, errors.New("Currently only one client per backend is supported")
	}
	c := &client{
		backend:     b,
		constraints: make(map[string]*constraintEntry),
	}
	var errs Errors
	for _, opt := range opts {
		if err := opt(c); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	if len(c.targets) == 0 {
		return nil, errors.New("No targets registered. Please register a target via client.Targets()")
	}
	if err := b.driver.Init(context.Background()); err != nil {
		return nil, err
	}
	if err := c.init(); err != nil {
		return nil, err
	}
	return c, nil
}
