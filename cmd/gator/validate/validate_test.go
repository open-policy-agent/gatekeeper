package validate

import "testing"

func Test(t *testing.T) {
	tcs := []struct {
		name string
	}{
		{},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
		})
	}
}
