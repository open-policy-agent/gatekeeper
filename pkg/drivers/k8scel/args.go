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
