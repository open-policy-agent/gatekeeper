package unversioned

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
)

var (
	ErrInvalidAssignField  = errors.New("invalid assign field")
	ErrInvalidFromMetadata = errors.New("invalid fromMetadata field")
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
	Value *types.Anything `json:"value,omitempty"`

	// FromMetadata assigns a value from the specified metadata field.
	FromMetadata *FromMetadata `json:"fromMetadata,omitempty"`

	// ExternalData describes the external data provider to be used for mutation.
	ExternalData *ExternalData `json:"externalData,omitempty"`
}

func (a *AssignField) GetValue(mutable *types.Mutable) (interface{}, error) {
	if a == nil {
		return nil, fmt.Errorf("assign is nil: %w", ErrInvalidAssignField)
	}
	if a.FromMetadata != nil {
		return a.FromMetadata.GetValue(mutable.Object)
	}
	if a.Value != nil {
		return a.Value.GetValue(), nil
	}

	placeholder := a.ExternalData.GetPlaceholder()
	if placeholder.Ref.DataSource == types.DataSourceUsername {
		placeholder.ValueAtLocation = mutable.Username
	}

	return placeholder, nil
}

func (a *AssignField) Validate() error {
	if a == nil {
		return fmt.Errorf("assign is nil: %w", ErrInvalidAssignField)
	}
	if a.Value == nil && a.FromMetadata == nil && a.ExternalData == nil {
		return fmt.Errorf("assign must set one of `externalData`, `value` or `fromMetadata`: %w", ErrInvalidAssignField)
	}

	populated := false
	for _, field := range []interface{}{a.Value, a.FromMetadata, a.ExternalData} {
		if !reflect.ValueOf(field).IsNil() {
			if populated {
				return fmt.Errorf("assign must only set one of `externalData`, `value` or `fromMetadata`: %w", ErrInvalidAssignField)
			}
			populated = true
		}
	}

	if a.FromMetadata != nil {
		return a.FromMetadata.Validate()
	}

	if a.ExternalData != nil {
		return a.ExternalData.Validate()
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
		return "", fmt.Errorf("attempted to fetch unknown metadata field %s: %w", fm.Field, ErrInvalidFromMetadata)
	}
}

func (fm *FromMetadata) Validate() error {
	if fm == nil {
		return fmt.Errorf("fromMetadata is nil: %w", ErrInvalidFromMetadata)
	}
	if !validFields[fm.Field] {
		return fmt.Errorf("field %s is not recognized: %w", fm.Field, ErrInvalidFromMetadata)
	}
	return nil
}
