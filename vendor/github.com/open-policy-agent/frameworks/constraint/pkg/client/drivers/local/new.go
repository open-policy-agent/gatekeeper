package local

import (
	"github.com/open-policy-agent/opa/rego"
	opatypes "github.com/open-policy-agent/opa/types"
)

// New constructs a new Driver and registers the built-in external_data function
// to OPA.
func New(args ...Arg) (*Driver, error) {
	d := &Driver{}
	for _, arg := range args {
		err := arg(d)
		if err != nil {
			return nil, err
		}
	}

	err := Defaults()(d)
	if err != nil {
		return nil, err
	}

	if d.providerCache != nil {
		rego.RegisterBuiltin1(
			&rego.Function{
				Name:    "external_data",
				Decl:    opatypes.NewFunction(opatypes.Args(opatypes.A), opatypes.A),
				Memoize: true,
			},
			externalDataBuiltin(d),
		)
	}

	return d, nil
}
