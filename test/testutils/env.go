package testutils

import (
	"os"
	"testing"
)

func Setenv(t *testing.T, key, value string) {
	old := os.Getenv(key)
	err := os.Setenv(key, value)
	if err != nil {
		t.Fatalf("setting env variable %q: %v", key, err)
	}

	t.Cleanup(func() {
		if old == "" {
			err = os.Unsetenv(key)
		} else {
			err = os.Setenv(key, old)
		}
		if err != nil {
			t.Errorf("resetting env variable %q: %v", key, err)
		}
	})
}
