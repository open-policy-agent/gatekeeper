package v1alpha1

import (
	"errors"
	"fmt"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/runtime"
)

type Field string

const (
	// ObjNamespace => metadata.namespace.
	ObjNamespace = Field("namespace")

	// ObjName => metadata.name.
	ObjName = Field("name")
)

var validFields = map[Field]bool{
	ObjNamespace: true,
	ObjName:      true,
}

type AssignField struct {
	// Value is a constant value that will be assigned to `location`
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:XPreserveUnknownFields
	Value *Anything `json:"value,omitempty"`

	// FromMetadata assigns a value from the specified metadata field.
	FromMetadata *FromMetadata `json:"fromMetadata,omitempty"`
}

func (a *AssignField) GetValue(metadata types.MetadataGetter) (interface{}, error) {
	if a.FromMetadata != nil {
		return a.FromMetadata.GetValue(metadata)
	}
	return runtime.DeepCopyJSONValue(a.Value.Value), nil
}

func (a *AssignField) Validate() error {
	if a.Value == nil && a.FromMetadata == nil {
		return errors.New("assign must have one of `value` or `fromMetadata` set")
	}

	if a.Value != nil && a.FromMetadata != nil {
		return errors.New("assign must only have one of `value` or `fromMetadata` set")
	}

	if a.FromMetadata != nil {
		return a.FromMetadata.Validate()
	}

	return nil
}

type FromMetadata struct {
	// Field specifies which metadata field provides the assigned value. Valid fields are `namespace` and `name`.
	Field Field `json:"field,omitempty"`
}

func (fm *FromMetadata) GetValue(obj types.MetadataGetter) (string, error) {
	switch fm.Field {
	case ObjNamespace:
		return obj.GetNamespace(), nil
	case ObjName:
		return obj.GetName(), nil
	default:
		return "", fmt.Errorf("attempted to fetch unknown metadata field %s", fm.Field)
	}
}

func (fm *FromMetadata) Validate() error {
	if !validFields[fm.Field] {
		return fmt.Errorf("field %s is not recognized", fm.Field)
	}
	return nil
}
