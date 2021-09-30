package templates

import (
	"fmt"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"sigs.k8s.io/yaml"
)

// ConstraintTemplateSchemas are the per-version structural schemas for
// ConstraintTemplates.
var ConstraintTemplateSchemas map[string]*schema.Structural

func initializeCTSchemaMap() {
	// Setup the CT Schema map for use in generalized defaulting functions
	ConstraintTemplateSchemas = make(map[string]*schema.Structural)

	// Ingest the constraint template CRD for use in defaulting functions
	constraintTemplateCRD := &apiextensionsv1.CustomResourceDefinition{}
	if err := yaml.Unmarshal([]byte(constraintTemplateCRDYaml), constraintTemplateCRD); err != nil {
		panic(fmt.Errorf("%w: failed to unmarshal yaml into constraintTemplateCRD", err))
	}

	// Fill version map with Structural types derived from ConstraintTemplate versions
	for _, crdVersion := range constraintTemplateCRD.Spec.Versions {
		versionlessSchema := &apiextensions.JSONSchemaProps{}
		err := Scheme.Convert(crdVersion.Schema.OpenAPIV3Schema, versionlessSchema, nil)
		if err != nil {
			panic(fmt.Errorf("%w: failed to convert JSONSchemaProps for ConstraintTemplate version %v", err, crdVersion.Name))
		}

		structural, err := schema.NewStructural(versionlessSchema)
		if err != nil {
			panic(fmt.Errorf("%w: failed to create Structural for ConstraintTemplate version %v", err, crdVersion.Name))
		}

		ConstraintTemplateSchemas[crdVersion.Name] = structural
	}
}
