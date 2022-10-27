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
		includeTrace   bool
		expectedOutput string
	}{
		{
			name: "default output",
			// inputFormat: "default", // note that the inputFormat defaults to "default"
			input: []*test.GatorResult{{
				Result:          fooRes,
				ViolatingObject: barObject,
				Trace:           &xyzTrace,
			}},
			includeTrace:   true,
			expectedOutput: "[\"\"] Message: \"\" \nTrace: xyz",
		},
		{
			name:        "yaml output",
			inputFormat: "YaML",
			input: []*test.GatorResult{{
				Result:          fooRes,
				ViolatingObject: barObject,
				Trace:           &xyzTrace,
			}},
			includeTrace: true,
			expectedOutput: "- result:\n    target: foo\n    msg: \"\"\n    metadata: {}\n    constraint:\n" +
				"        object:\n            kind: kind\n    enforcementaction: \"\"\n  violatingObject:\n    " +
				"bar: xyz\n  trace: xyz\n",
		},
		{
			name:        "json output",
			inputFormat: "jSOn",
			input: []*test.GatorResult{{
				Result:          fooRes,
				ViolatingObject: barObject,
				Trace:           &xyzTrace,
			}},
			includeTrace: false,
			expectedOutput: "[\n" +
				"    {\n" +
				"        \"target\": \"foo\",\n" +
				"        \"constraint\": {\n" +
				"            \"kind\": \"kind\"\n" +
				"        },\n" +
				"        \"violatingObject\": {\n" +
				"            \"bar\": \"xyz\"\n" +
				"        },\n" +
				"        \"Trace\": \"xyz\"\n" +
				"    }\n" +
				"]",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			output := formatOutput(tc.inputFormat, tc.includeTrace, tc.input)
			if diff := cmp.Diff(tc.expectedOutput, output); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
