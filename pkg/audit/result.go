package audit

import (
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Result struct {
	*types.Result
	obj *unstructured.Unstructured
}

func ToResults(obj *unstructured.Unstructured, resp *types.Responses) []Result {
	var results []Result

	for _, r := range resp.Results() {
		results = append(results, Result{
			Result: r,
			obj:    obj,
		})
	}

	return results
}
