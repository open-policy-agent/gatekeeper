package k8scel

func New(args ...Arg) (*Driver, error) {
	driver := &Driver{
		validators: map[string]*validatorWrapper{},
	}
	for _, arg := range args {
		if err := arg(driver); err != nil {
			return nil, err
		}
	}
	return driver, nil
}
