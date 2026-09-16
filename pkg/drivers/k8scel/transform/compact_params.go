package transform

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	celgo "github.com/google/cel-go/cel"
)

type parameterLiteral struct {
	expression string
	valueType  *celgo.Type
	removedDyn int
}

func compactParametersExpression(value interface{}) (string, error) {
	literal, err := compactParameterLiteral(value)
	if err != nil {
		return "", err
	}
	if literal.removedDyn <= 1 {
		return jsonValueToCEL(value)
	}
	return "dyn(" + literal.expression + ")", nil
}

func compactParameterLiteral(value interface{}) (parameterLiteral, error) {
	switch typed := value.(type) {
	case []interface{}:
		children := make([]parameterLiteral, len(typed))
		for index, item := range typed {
			child, err := compactParameterLiteral(item)
			if err != nil {
				return parameterLiteral{}, fmt.Errorf("list item %d: %w", index, err)
			}
			children[index] = child
		}
		expressions, elementType, removed := compactAggregateChildren(children)
		return parameterLiteral{"[" + strings.Join(expressions, ", ") + "]", celgo.ListType(elementType), removed}, nil
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		children := make([]parameterLiteral, len(keys))
		encodedKeys := make([]string, len(keys))
		for index, key := range keys {
			encoded, err := json.Marshal(key)
			if err != nil {
				return parameterLiteral{}, err
			}
			encodedKeys[index] = string(encoded)
			child, err := compactParameterLiteral(typed[key])
			if err != nil {
				return parameterLiteral{}, fmt.Errorf("property %q: %w", key, err)
			}
			children[index] = child
		}
		expressions, elementType, removed := compactAggregateChildren(children)
		for index := range expressions {
			expressions[index] = encodedKeys[index] + ": " + expressions[index]
		}
		return parameterLiteral{"{" + strings.Join(expressions, ", ") + "}", celgo.MapType(celgo.StringType, elementType), removed}, nil
	default:
		expression, err := jsonValueToCEL(value)
		if err != nil {
			return parameterLiteral{}, err
		}
		var valueType *celgo.Type
		switch typed := value.(type) {
		case nil:
			valueType = celgo.NullType
		case bool:
			valueType = celgo.BoolType
		case string:
			valueType = celgo.StringType
		case int, int8, int16, int32, int64:
			valueType = celgo.IntType
		case uint, uint8, uint16, uint32, uint64:
			valueType = celgo.UintType
		case float32, float64:
			valueType = celgo.DoubleType
		case json.Number:
			valueType = celgo.IntType
			if strings.ContainsAny(string(typed), ".eE") {
				valueType = celgo.DoubleType
			}
		default:
			return parameterLiteral{}, fmt.Errorf("unsupported JSON value type %T", value)
		}
		return parameterLiteral{expression, valueType, 0}, nil
	}
}

func compactAggregateChildren(children []parameterLiteral) ([]string, *celgo.Type, int) {
	elementType := celgo.DynType
	if len(children) > 0 {
		elementType = children[0].valueType
	}
	mixed := false
	for _, child := range children {
		if !elementType.IsExactType(child.valueType) {
			mixed = true
		}
	}
	if mixed {
		elementType = celgo.DynType
	}
	expressions := make([]string, len(children))
	removed := 0
	for index, child := range children {
		expressions[index] = child.expression
		removed += child.removedDyn
		if mixed {
			expressions[index] = "dyn(" + child.expression + ")"
		} else {
			removed++
		}
	}
	return expressions, elementType, removed
}
