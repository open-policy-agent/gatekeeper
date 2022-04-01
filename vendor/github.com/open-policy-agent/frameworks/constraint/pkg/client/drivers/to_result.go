package drivers

import (
	"errors"
	"fmt"

	apiconstraints "github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
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

	resultMapBinding, found := r.Bindings["result"]
	if !found {
		return nil, errors.New("no binding for result")
	}

	resultMap, ok := resultMapBinding.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("result binding was %T but want %T",
			resultMapBinding, map[string]interface{}{})
	}

	messageBinding, found := resultMap["msg"]
	if !found {
		return nil, errors.New("no binding for msg")
	}

	message, ok := messageBinding.(string)
	if !ok {
		return nil, fmt.Errorf("message binding was %T but want %T",
			messageBinding, "")
	}
	result.Msg = message

	result.Metadata = map[string]interface{}{
		"details": resultMap["details"],
	}

	keyBinding, found := resultMap["key"]
	if !found {
		return nil, errors.New("no binding for Constraint key")
	}

	keyMap, ok := keyBinding.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("key binding was %T but want %T",
			keyBinding, map[string]interface{}{})
	}

	key := ConstraintKey{
		Kind: keyMap["kind"].(string),
		Name: keyMap["name"].(string),
	}

	constraint := constraints[key]
	// DeepCopy the result so we don't leak internal state.
	result.Constraint = constraint.DeepCopy()

	enforcementAction, found, err := unstructured.NestedString(constraint.Object, "spec", "enforcementAction")
	if err != nil {
		return nil, err
	}
	if !found {
		enforcementAction = apiconstraints.EnforcementActionDeny
	}

	result.EnforcementAction = enforcementAction

	return result, nil
}
