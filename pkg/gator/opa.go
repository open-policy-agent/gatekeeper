package gator

import (
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
)

func NewOPAClient(includeTrace bool) (Client, error) {
	driver, err := rego.New(rego.Tracing(includeTrace))
	if err != nil {
		return nil, err
	}

	c, err := constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver))
	if err != nil {
		return nil, err
	}

	return c, nil
}
