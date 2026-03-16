package crds

import (
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/utils/ptr"
)

// CreateSchema combines the schema of the match target and the ConstraintTemplate parameters
// to form the schema of the actual constraint resource.
func CreateSchema(templ *templates.ConstraintTemplate, target MatchSchemaProvider) *apiextensions.JSONSchemaProps {
	defaultEnforcementAction := apiextensions.JSON("deny")
	props := map[string]apiextensions.JSONSchemaProps{
		"match":             target.MatchSchema(),
		"enforcementAction": {Type: "string", Default: &defaultEnforcementAction},
		"scopedEnforcementActions": {
			Type:    "array",
			Default: nil,
			Items: &apiextensions.JSONSchemaPropsOrArray{
				Schema: &apiextensions.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensions.JSONSchemaProps{
						"action": {Type: "string"},
						"enforcementPoints": {
							Type: "array",
							Items: &apiextensions.JSONSchemaPropsOrArray{
								Schema: &apiextensions.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensions.JSONSchemaProps{
										"name": {Type: "string"},
									},
								},
							},
						},
					},
				},
			},
		},
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
						MaxLength: ptr.To[int64](63),
					},
				},
			},
			"spec": {
				Type:       "object",
				Properties: props,
			},
			"status": {
				XPreserveUnknownFields: ptr.To[bool](true),
			},
		},
	}

	return schema
}
