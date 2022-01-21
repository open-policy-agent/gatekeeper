package crds

import (
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/utils/pointer"
)

// CreateSchema combines the schema of the match target and the ConstraintTemplate parameters
// to form the schema of the actual constraint resource.
func CreateSchema(templ *templates.ConstraintTemplate, target MatchSchemaProvider) *apiextensions.JSONSchemaProps {
	props := map[string]apiextensions.JSONSchemaProps{
		"match":             target.MatchSchema(),
		"enforcementAction": {Type: "string"},
	}

	if templ.Spec.CRD.Spec.Validation != nil && templ.Spec.CRD.Spec.Validation.OpenAPIV3Schema != nil {
		internalSchema := *templ.Spec.CRD.Spec.Validation.OpenAPIV3Schema.DeepCopy()
		props["parameters"] = internalSchema
	}

	schema := &apiextensions.JSONSchemaProps{
		Type: "object",
		Properties: map[string]apiextensions.JSONSchemaProps{
			"metadata": {
				Type: "object",
				Properties: map[string]apiextensions.JSONSchemaProps{
					"name": {
						Type:      "string",
						MaxLength: pointer.Int64(63),
					},
				},
			},
			"spec": {
				Type:       "object",
				Properties: props,
			},
			"status": {
				XPreserveUnknownFields: pointer.BoolPtr(true),
			},
		},
	}

	return schema
}
