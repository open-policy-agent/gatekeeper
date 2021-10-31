package testutils

import (
	"os"
	"testing"
)

// Setenv sets os environment variable key to value.
// Registers the environment variable to be set to its original value at the end
// of the test.
//
// This prevents cross-talk between tests, as some may implicitly come to rely on other tests running before them in
// order to succeed.
func Setenv(t *testing.T, key, value string) {
	old, set := os.LookupEnv(key)
	err := os.Setenv(key, value)
	if err != nil {
		t.Fatalf("setting env variable %q: %v", key, err)
	}

	t.Cleanup(func() {
		if !set {
			err = os.Unsetenv(key)
		} else {
			err = os.Setenv(key, old)
		}
		if err != nil {
			t.Errorf("resetting env variable %q: %v", key, err)
		}
	})
}
