package gator

import (
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"io"
)

type Opt func() ([]constraintclient.Opt, []rego.Arg, error)

func NewOPAClient(includeTrace bool, opts ...Opt) (Client, error) {
	args := []constraintclient.Opt{constraintclient.Targets(&target.K8sValidationTarget{})}

	driverArgs := []rego.Arg{
		rego.Tracing(includeTrace),
	}

	for _, opt := range opts {
		extraArgs, extraDriverArgs, err := opt()
		if err != nil {
			return nil, err
		}

		args = append(args, extraArgs...)
		driverArgs = append(driverArgs, extraDriverArgs...)
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

func WithK8sCEL() Opt {
	return func() ([]constraintclient.Opt, []rego.Arg, error) {
		k8sDriver, err := k8scel.New()
		if err != nil {
			return nil, nil, err
		}

		return []constraintclient.Opt{
			constraintclient.Driver(k8sDriver),
		}, []rego.Arg{}, nil
	}
}

func WithPrintHook(w io.Writer) Opt {
	return func() ([]constraintclient.Opt, []rego.Arg, error) {
		return []constraintclient.Opt{}, []rego.Arg{
			rego.PrintEnabled(true),
			rego.PrintHook(NewPrintHook(w)),
		}, nil
	}
}
