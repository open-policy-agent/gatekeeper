package audit

import (
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestResult_ToResult(t *testing.T) {
	t.Run("transforms results as expected", func(t *testing.T) {
		aResult := types.Result{
			Target:            "targetA",
			Msg:               "violationA",
			EnforcementAction: "deny",
		}

		responses := types.Responses{
			ByTarget: map[string]*types.Response{
				"targetA": {
					Target: "targetA",
					Results: []*types.Result{
						&aResult,
					},
				},
			},
		}

		obj := &unstructured.Unstructured{}
		obj.SetName("test")
		obj.SetNamespace("test-ns")
		toResults := ToResults(obj, &responses)

		expectedWrappedResult := Result{
			Result: &aResult,
			obj:    obj,
		}

		require.Len(t, toResults, 1)
		require.Equal(t, expectedWrappedResult, toResults[0], "expected results to be the same")
	})

	t.Run("handles empty responses", func(t *testing.T) {
		emptyResponses := &types.Responses{}
		emptyObj := &unstructured.Unstructured{}
		require.Len(t, ToResults(emptyObj, emptyResponses), 0)
	})

	t.Run("hadles nil responses", func(t *testing.T) {
		require.Len(t, ToResults(nil, nil), 0)
	})
}
