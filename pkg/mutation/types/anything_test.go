package types

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestAnything(t *testing.T) {
	vals := []struct {
		name string
		val  interface{}
	}{
		{
			name: "simple string",
			val:  "a",
		},
		{
			name: "number",
			val:  float64(74567),
		},
		{
			name: "array",
			val:  []interface{}{"a", "b", "c"},
		},
		{
			name: "object",
			val: map[string]interface{}{
				"yes": true,
				"no":  false,
			},
		},
	}
	for _, tc := range vals {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.val)
			if err != nil {
				t.Fatalf("error marshaling value: %v", err)
			}

			obj := &Anything{}
			if err := json.Unmarshal(b, obj); err != nil {
				t.Fatalf("error unmarshaling value: %v", err)
			}
			if diff := cmp.Diff(tc.val, obj.Value); diff != "" {
				t.Errorf("bad round-trip conversion:\n%v", diff)
			}
		})
	}
}
