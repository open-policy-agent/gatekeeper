package validate

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/pkg/gator/fixtures"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestValidate(t *testing.T) {
	tcs := []struct {
		name      string
		inputs    []string
		responses *types.Responses
		err       bool
	}{
		{
			name: "basic no violation",
			inputs: []string{
				fixtures.TemplateAlwaysValidate,
				fixtures.ConstraintAlwaysValidate,
				fixtures.ObjectMultiple,
			},
		},
		{
			name: "basic violation",
			inputs: []string{
				fixtures.TemplateNeverValidate,
				fixtures.ConstraintNeverValidate,
				fixtures.ObjectMultiple,
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// convert the test resources to unstructureds
			var objs []*unstructured.Unstructured
			for _, input := range tc.inputs {
				u, err := readUnstructured(input)
				if err != nil {
					t.Fatalf("readUnstructured: %v", err)
				}
				objs = append(objs, u)
			}

			resps, err := Validate(objs)
			if tc.err {
				if err == nil {
					t.Errorf("got nil err, want err")
				}
			} else {
				if err != nil {
					t.Errorf("got err '%v', want nil", err)
				}
			}

			got := resps.Results()
			want := tc.responses.Results()

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Validate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// I copied these helper functions from Will's code.  Should consider factoring
// them into a shared place.
func readUnstructured(str string) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{
		Object: make(map[string]interface{}),
	}
	err := parseYAML([]byte(str), u)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func parseYAML(yamlBytes []byte, v interface{}) error {
	// Pass through JSON since k8s parsing logic doesn't fully handle objects
	// parsed directly from YAML. Without passing through JSON, the OPA client
	// panics when handed scalar types it doesn't recognize.
	obj := make(map[string]interface{})

	err := yaml.Unmarshal(yamlBytes, obj)
	if err != nil {
		return err
	}

	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return err
	}

	return parseJSON(jsonBytes, v)
}

func parseJSON(jsonBytes []byte, v interface{}) error {
	return json.Unmarshal(jsonBytes, v)
}
