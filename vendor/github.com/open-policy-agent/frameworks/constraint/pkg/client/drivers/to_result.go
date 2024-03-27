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

func ToResults(constraints map[ConstraintKey]*unstructured.Unstructured, resultSet rego.ResultSet, sourceEP string) ([]*types.Result, error) {
	var results []*types.Result
	for _, r := range resultSet {
		result, err := ToResult(constraints, r, sourceEP)
		if err != nil {
			return nil, err
		}
		results = append(results, result...)
	}

	return results, nil
}

func ToResult(constraints map[ConstraintKey]*unstructured.Unstructured, r rego.Result, sourceEP string) ([]*types.Result, error) {
	results := []*types.Result{}

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

	enforcementAction, err := apiconstraints.GetEnforcementAction(constraint)
	if err != nil {
		return nil, err
	}

	actions := []string{enforcementAction}
	if apiconstraints.IsEnforcementActionScoped(enforcementAction) {
		actions, err = apiconstraints.GetEnforcementActionsForEP(constraint, sourceEP)
		if err != nil {
			return nil, err
		}
	}

	for _, action := range actions {
		result := &types.Result{}
		result.Msg = message
		// DeepCopy the result so we don't leak internal state.
		result.Constraint = constraint.DeepCopy()
		result.EnforcementAction = action
		result.Metadata = map[string]interface{}{
			"details": resultMap["details"],
		}

		results = append(results, result)
	}

	return results, nil
}
