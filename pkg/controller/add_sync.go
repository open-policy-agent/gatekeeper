package controller

import (
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/sync"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	Injectors = append(Injectors, &sync.Adder{})
}
