package test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/test"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Test_formatOutput makes sure that the formatted output of `gator test`
// is consistent as we iterate over the tool. The purpose of this test IS NOT
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
			expectedOutput: `/ : [""] Message: ""
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
    scopedenforcementactions: []
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
			output := formatOutput(tc.inputFormat, tc.input, nil)
			if diff := cmp.Diff(tc.expectedOutput, output); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

// Test_enforcableFailures makes sure that the `gator test` is able to detect
// denied constraints when if found some.
func Test_enforcableFailures(t *testing.T) {
	testCases := []struct {
		name           string
		input          []*test.GatorResult
		expectedOutput bool
	}{
		{
			name: "don't fail on warn action",
			input: []*test.GatorResult{{
				Result: types.Result{
					EnforcementAction: string(util.Warn),
				},
			}},
			expectedOutput: false,
		},
		{
			name: "fail on deny action",
			input: []*test.GatorResult{{
				Result: types.Result{
					EnforcementAction: string(util.Deny),
				},
			}},
			expectedOutput: true,
		},
		{
			name: "fail if at least one scoped deny action",
			input: []*test.GatorResult{{
				Result: types.Result{
					EnforcementAction: string(util.Dryrun),
					ScopedEnforcementActions: []string{
						string(util.Scoped),
						string(util.Deny),
					},
				},
			}},
			expectedOutput: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if ok := enforceableFailures(tc.input); ok != tc.expectedOutput {
				t.Fatalf("unexpected output: %v", ok)
			}
		})
	}
}
