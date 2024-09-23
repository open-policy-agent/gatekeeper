package drivers

import (
	"errors"
	"fmt"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/opa/rego"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func KeyMap(constraints []*unstructured.Unstructured) map[ConstraintKey]*unstructured.Unstructured {
	result := make(map[ConstraintKey]*unstructured.Unstructured)

	for _, constraint := range constraints {
		key := ConstraintKeyFrom(constraint)
		result[key] = constraint
	}

	return result
}

func ToResults(constraints map[ConstraintKey]*unstructured.Unstructured, resultSet rego.ResultSet) ([]*types.Result, error) {
	var results []*types.Result
	for _, r := range resultSet {
		result, err := ToResult(constraints, r)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	return results, nil
}

func ToResult(constraints map[ConstraintKey]*unstructured.Unstructured, r rego.Result) (*types.Result, error) {
	result := &types.Result{}

	resultMap, found, err := unstructured.NestedMap(r.Bindings, "result")
	if err != nil {
		return nil, fmt.Errorf("extracting result binding: %v", err)
	}

	if !found {
		return nil, errors.New("no binding for result")
	}

	message, found, err := unstructured.NestedString(resultMap, "msg")
	if err != nil {
		return nil, fmt.Errorf("extracting message binding: %v", err)
	}

	if !found {
		return nil, errors.New("no binding for msg")
	}

	result.Msg = message

	result.Metadata = map[string]interface{}{
		"details": resultMap["details"],
	}

	keyMap, found, err := unstructured.NestedStringMap(resultMap, "key")
	if err != nil {
		return nil, fmt.Errorf("extracting key binding: %v", err)
	}

	if !found {
		return nil, errors.New("no binding for Constraint key")
	}

	key := ConstraintKey{
		Kind: keyMap["kind"],
		Name: keyMap["name"],
	}
	constraint := constraints[key]

	// DeepCopy the result so we don't leak internal state.
	result.Constraint = constraint.DeepCopy()

	return result, nil
}
