package k8scel

type Arg func(*Driver) error

// GatherStats starts collecting various stats around the
// underlying engine's calls.
func GatherStats() Arg {
	return func(driver *Driver) error {
		driver.gatherStats = true

		return nil
	}
}

// VAPGenerationDefault sets the expected default
// value for the generateVAP field.
// If no value is provided, VAP generation
// is presumed to be disabled and the engine will
// validate ALL policies. Otherwise, the engine
// will only validate policies not expected to be
// enforced via VAP.
func VAPGenerationDefault(d bool) Arg {
	return func(driver *Driver) error {
		driver.generateVAPDefault = d
		return nil
	}
}
