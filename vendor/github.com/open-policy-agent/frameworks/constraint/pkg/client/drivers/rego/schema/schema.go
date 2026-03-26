// Package schema provides Rego driver source schema handling.
package schema

import (
	"errors"
	"fmt"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Name is the name of the driver.
const Name = "Rego"

var (
	// ErrBadType is returned when the source type cannot be recognized.
	ErrBadType = errors.New("could not recognize the type")
	// ErrMissingField is returned when a required field is missing from the rego source.
	ErrMissingField = errors.New("rego source missing required field")
)

// Source represents the Rego source code configuration for a constraint template.
type Source struct {
	// Rego holds the main code for the constraint template. The `Violations` rule is the entry point.
	Rego string `json:"rego,omitempty"`
	// Version holds the version of the Rego code supplied in `Rego`.
	Version string `json:"version,omitempty"`
	// Libs holds supporting code for the main rego library. Modules can be imported from `data.libs`.
	Libs []string `json:"libs,omitempty"`
}

// ToUnstructured converts the Source to an unstructured map representation.
func (in *Source) ToUnstructured() map[string]interface{} {
	if in == nil {
		return nil
	}

	out := map[string]interface{}{}

	out["rego"] = in.Rego
	out["version"] = in.Version

	if in.Libs != nil {
		var libs []interface{}
		for _, v := range in.Libs {
			libs = append(libs, v)
		}
		out["libs"] = libs
	}

	return out
}

// GetSource extracts Source from a templates.Code object.
func GetSource(code templates.Code) (*Source, error) {
	rawCode := code.Source
	v, ok := rawCode.Value.(map[string]interface{})
	if !ok {
		return nil, ErrBadType
	}

	source := &Source{}

	rego, found, err := unstructured.NestedString(v, "rego")
	if err != nil {
		return nil, fmt.Errorf("%w: while extracting Rego source", err)
	}
	if !found {
		return nil, fmt.Errorf("%w: rego", ErrMissingField)
	}

	source.Rego = rego

	version, found, err := unstructured.NestedString(v, "version")
	if err != nil {
		return nil, fmt.Errorf("%w: while extracting Rego version", err)
	}
	if !found || version == "" {
		version = "v0"
	}

	source.Version = version

	libs, found, err := unstructured.NestedStringSlice(v, "libs")
	if err != nil {
		return nil, fmt.Errorf("%w: while extracting Rego libs", err)
	}
	if found {
		source.Libs = libs
	}

	return source, nil
}
