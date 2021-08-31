package uint64bool

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Uint64OrBool is a type that can hold a uint64 or a bool. When used in JSON or
// YAML marshaling and unmarshaling, it produces or consumes the inner type.
// This types allows you to have, for example, a JSON field that can accept a
// true/false value or a number.
type Uint64OrBool struct {
	Type      Type
	Uint64Val uint64
	BoolVal   bool
}

var (
	_ yaml.Unmarshaler = &Uint64OrBool{}
	_ yaml.Marshaler   = &Uint64OrBool{}
	_ json.Unmarshaler = &Uint64OrBool{}
	_ json.Marshaler   = &Uint64OrBool{}
)

func FromUint64(val uint64) *Uint64OrBool {
	return &Uint64OrBool{Type: Uint64, Uint64Val: val}
}

func FromBool(val bool) *Uint64OrBool {
	return &Uint64OrBool{Type: Bool, BoolVal: val}
}

func (v *Uint64OrBool) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("%w but got a non-scalar type", ErrInvalidUint64OrBool)
	}

	switch value.Tag {
	case "!!bool":
		v.Type = Bool
		return value.Decode(&v.BoolVal)
	case "!!int":
		var val int
		err := value.Decode(&val)
		if err != nil {
			// Can only happen if UnmarshalYAML is called on an improperly-constructed
			// Node.
			return err
		}
		return v.unmarshalInt(val)
	default:
		return fmt.Errorf("%w but got %s", ErrInvalidUint64OrBool, value.Tag[2:])
	}
}

func (v *Uint64OrBool) unmarshalInt(val int) error {
	v.Type = Uint64

	if val < 0 {
		return fmt.Errorf("%w but got negative integer %d",
			ErrInvalidUint64OrBool, val)
	}

	v.Uint64Val = uint64(val)
	return nil
}

func (v *Uint64OrBool) MarshalYAML() (interface{}, error) {
	switch v.Type {
	case Bool:
		return v.BoolVal, nil
	case Uint64:
		if v.Uint64Val > math.MaxInt64 {
			return nil, fmt.Errorf("%w: %d is larger than maximum allowed integer %d",
				ErrInvalidUint64OrBool, v.Uint64Val, math.MaxInt64)
		}
		return int(v.Uint64Val), nil
	default:
		return nil, fmt.Errorf("%w: type must be %d or %d",
			ErrInvalidUint64OrBool, Uint64, Bool)
	}
}

func (v *Uint64OrBool) UnmarshalJSON(bytes []byte) error {
	// Assumes JSON is UTF-8 encoded.
	if unicode.IsDigit(rune(bytes[0])) {
		v.Type = Uint64
		if err := json.Unmarshal(bytes, &v.Uint64Val); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidUint64OrBool, err)
		}

		return nil
	}

	v.Type = Bool
	if err := json.Unmarshal(bytes, &v.BoolVal); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidUint64OrBool, err)
	}

	return nil
}

func (v *Uint64OrBool) MarshalJSON() ([]byte, error) {
	switch v.Type {
	case Uint64:
		if v.Uint64Val > math.MaxInt64 {
			return nil, fmt.Errorf("%w: %d is larger than maximum allowed integer %d",
				ErrInvalidUint64OrBool, v.Uint64Val, math.MaxInt64)
		}
		return json.Marshal(v.Uint64Val)
	case Bool:
		return json.Marshal(v.BoolVal)
	default:
		return []byte{}, fmt.Errorf("%w: type must be %d or %d",
			ErrInvalidUint64OrBool, Uint64, Bool)
	}
}

// Type is an enum of the valid underlying types for a Uint64OrBool.
type Type int64

// The valid Types.
const (
	Uint64 Type = iota
	Bool
)

// ErrInvalidUint64OrBool indicates the value could not be parsed to a
// Uint64OrBool.
var ErrInvalidUint64OrBool = errors.New("field must be a non-negative integer or a boolean")
