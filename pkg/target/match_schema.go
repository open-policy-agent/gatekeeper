package target

import (
	"fmt"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

var matchJSONSchemaProps apiextensions.JSONSchemaProps

func init() {
	matchCRD := &apiextensionsv1.CustomResourceDefinition{}
	if err := yaml.Unmarshal([]byte(matchYAML), matchCRD); err != nil {
		panic(fmt.Errorf("failed to unmarshal match yaml: %w", err))
	}

	// Sanity checks to ensure the CRD was generated properly
	if len(matchCRD.Spec.Versions) == 0 {
		panic(fmt.Errorf("generated match CRD does not contain any versions"))
	}
	if matchCRD.Spec.Versions[0].Schema.OpenAPIV3Schema == nil {
		panic(fmt.Errorf("generated match CRD has nil OpenAPIV3Schema"))
	}

	// Convert v1 JSONSchemaProps to versionless
	rt := runtime.NewScheme()
	if err := apiextensions.AddToScheme(rt); err != nil {
		panic(fmt.Errorf("could not add apiextensions to scheme: %w", err))
	}
	if err := apiextensionsv1.AddToScheme(rt); err != nil {
		panic(fmt.Errorf("could not add apiextensionsv1 to scheme: %w", err))
	}
	embedded := matchCRD.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["embeddedMatch"]
	if err := rt.Convert(&embedded, &matchJSONSchemaProps, nil); err != nil {
		panic(fmt.Errorf("could not convert match JSONSchemaProps from v1 to versionless: %w", err))
	}
}

func matchSchema() apiextensions.JSONSchemaProps {
	return matchJSONSchemaProps
}
