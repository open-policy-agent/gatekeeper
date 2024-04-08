package llm

func New(args ...Arg) (*Driver, error) {
	driver := &Driver{
		prompts: make(map[string]string),
	}
	for _, arg := range args {
		if err := arg(driver); err != nil {
			return nil, err
		}
	}
	return driver, nil
}
