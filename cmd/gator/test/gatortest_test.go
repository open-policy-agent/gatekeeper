package test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/pkg/gator/test"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Test_formatOutput makes sure that the formatted output of `gator test`
// is consitent as we iterate over the tool. The test IS NOT
// testing the `gator test` results themselves.
func Test_formatOutput(t *testing.T) {
	constraintObj := &unstructured.Unstructured{}
	constraintObj.SetKind("kind")

	fooRes := types.Result{
		Target:     "foo",
		Constraint: constraintObj,
	}
	barObject := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"bar":  "xyz",
			"kind": "kind",
		},
	}
	xyzTrace := "xyz"

	testCases := []struct {
		name           string
		inputFormat    string
		input          []*test.GatorResult
		expectedOutput string
	}{
		{
			name: "default output",
			// inputFormat: "default", // note that the inputFormat defaults to "default"/ human friendly
			input: []*test.GatorResult{{
				Result:          fooRes,
				ViolatingObject: barObject,
				Trace:           nil,
			}},
			expectedOutput: `[""] Message: "" 
`,
		},
		{
			name:        "yaml output",
			inputFormat: "YaML",
			input: []*test.GatorResult{{
				Result:          fooRes,
				ViolatingObject: barObject,
				Trace:           &xyzTrace,
			}},
			expectedOutput: `- result:
    target: foo
    msg: ""
    metadata: {}
    constraint:
        object:
            kind: kind
    enforcementaction: ""
  violatingObject:
    bar: xyz
  trace: xyz
`,
		},
		{
			name:        "json output",
			inputFormat: "jSOn",
			input: []*test.GatorResult{{
				Result:          fooRes,
				ViolatingObject: barObject,
				Trace:           &xyzTrace,
			}},
			expectedOutput: `[
    {
        "target": "foo",
        "constraint": {
            "kind": "kind"
        },
        "violatingObject": {
            "bar": "xyz",
            "kind": "kind"
        },
        "trace": "xyz"
    }
]`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.NotEmpty(t, formatOutput(tc.inputFormat, tc.input))

			switch strings.ToLower(tc.inputFormat) {
			case stringJSON:
				var gatorResult []*test.GatorResult
				require.NoError(t, json.Unmarshal([]byte(tc.expectedOutput), &gatorResult))

				if diff := cmp.Diff(tc.input, gatorResult, cmpopts.IgnoreFields(test.GatorResult{}, "Metadata")); diff != "" {
					t.Fatal(diff)
				}
			case stringYAML:
				var gatorResult []*test.GatorResult
				require.NoError(t, yaml.Unmarshal([]byte(tc.expectedOutput), &gatorResult))

				if diff := cmp.Diff(tc.input, gatorResult, cmpopts.IgnoreFields(test.GatorResult{}, "Metadata", "ViolatingObject")); diff != "" {
					t.Fatal(diff)
				}

			case stringHumanFriendly:
			default:
				require.Equal(t, tc.expectedOutput, formatOutput(tc.inputFormat, tc.input))
			}
		})
	}
}
