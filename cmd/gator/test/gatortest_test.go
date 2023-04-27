package test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/pkg/gator/test"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Test_formatOutput makes sure that the formatted output of `gator test`
// is consitent as we iterate over the tool. The purpose of this test IS NOT
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
			"bar": "xyz",
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
			// inputFormat: "default", // note that the inputFormat defaults to "default"
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
            "bar": "xyz"
        },
        "trace": "xyz"
    }
]`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			output := formatOutput(tc.inputFormat, tc.input)
			if diff := cmp.Diff(tc.expectedOutput, output); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
