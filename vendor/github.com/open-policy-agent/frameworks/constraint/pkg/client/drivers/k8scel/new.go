package k8scel

import (
	"k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/admission/plugin/validatingadmissionpolicy"
)

func New(args ...Arg) (*Driver, error) {
	driver := &Driver{
		validators:     map[string]validatingadmissionpolicy.Validator{},
		filterCompiler: cel.NewFilterCompiler(),
	}
	for _, arg := range args {
		if err := arg(driver); err != nil {
			return nil, err
		}
	}
	return driver, nil
}
