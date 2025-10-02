package gator

import (
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"os"
)

func NewOPAClient(includeTrace bool, printEnabled bool, k8sCEL bool) (Client, error) {
	args := []constraintclient.Opt{constraintclient.Targets(&target.K8sValidationTarget{})}

	if k8sCEL {
		k8sDriver, err := k8scel.New()
		if err != nil {
			return nil, err
		}
		args = append(args, constraintclient.Driver(k8sDriver))
	}

	driverArgs := []rego.Arg{
		rego.Tracing(includeTrace),
	}

	if printEnabled {
		driverArgs = append(driverArgs,
			rego.PrintEnabled(printEnabled),
			rego.PrintHook(NewPrintHook(os.Stdout)),
		)
	}

	driver, err := rego.New(driverArgs...)
	if err != nil {
		return nil, err
	}

	args = append(args, constraintclient.Driver(driver), constraintclient.EnforcementPoints([]string{util.GatorEnforcementPoint}...))

	c, err := constraintclient.NewClient(args...)
	if err != nil {
		return nil, err
	}

	return c, nil
}
