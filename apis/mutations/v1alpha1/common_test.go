package v1alpha1

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestAnything(t *testing.T) {
	vals := []interface{}{
		"a",
		float64(74567),
		[]interface{}{"a", "b", "c"},
		map[string]interface{}{
			"yes": true,
			"no":  false,
		},
	}
	for i, tc := range vals {
		t.Run(fmt.Sprintf("test #%d", i), func(t *testing.T) {
			b, err := json.Marshal(tc)
			if err != nil {
				t.Fatalf("error marshaling value: %v", err)
			}

			obj := &Anything{}
			if err := json.Unmarshal(b, obj); err != nil {
				t.Fatalf("error unmarshaling value: %v", err)
			}
			if !reflect.DeepEqual(tc, obj.Value) {
				t.Errorf("bad round-trip conversion. Want %+v, got %+v", tc, obj.Value)
			}
		})
	}
}
