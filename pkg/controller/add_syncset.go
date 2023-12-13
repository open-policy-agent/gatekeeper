package controller

import (
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/syncset"
)

func init() {
	Injectors = append(Injectors, &syncset.Adder{})
}
