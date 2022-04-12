package crds

import (
	"fmt"
	"strings"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1alpha1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
)

var scheme = runtime.NewScheme()

func init() {
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
}

// CreateCRD takes a template and a schema and converts it to a CRD.
func CreateCRD(templ *templates.ConstraintTemplate, schema *apiextensions.JSONSchemaProps) (*apiextensions.CustomResourceDefinition, error) {
	crd := &apiextensions.CustomResourceDefinition{
		Spec: apiextensions.CustomResourceDefinitionSpec{
			PreserveUnknownFields: pointer.Bool(false),
			Group:                 constraints.Group,
			Names: apiextensions.CustomResourceDefinitionNames{
				Kind:       templ.Spec.CRD.Spec.Names.Kind,
				ListKind:   templ.Spec.CRD.Spec.Names.Kind + "List",
				Plural:     strings.ToLower(templ.Spec.CRD.Spec.Names.Kind),
				Singular:   strings.ToLower(templ.Spec.CRD.Spec.Names.Kind),
				ShortNames: templ.Spec.CRD.Spec.Names.ShortNames,
				Categories: []string{
					"constraint",
					"constraints",
				},
			},
			Validation: &apiextensions.CustomResourceValidation{
				OpenAPIV3Schema: schema,
			},
			Scope:   apiextensions.ClusterScoped,
			Version: v1beta1.SchemeGroupVersion.Version,
			Subresources: &apiextensions.CustomResourceSubresources{
				Status: &apiextensions.CustomResourceSubresourceStatus{},
				Scale:  nil,
			},
			Versions: []apiextensions.CustomResourceDefinitionVersion{
				{
					Name:    v1beta1.SchemeGroupVersion.Version,
					Storage: true,
					Served:  true,
				},
				{
					Name:    v1alpha1.SchemeGroupVersion.Version,
					Storage: false,
					Served:  true,
				},
			},
			AdditionalPrinterColumns: []apiextensions.CustomResourceColumnDefinition{
				{
					Name:        "enforcement-action",
					Description: "Type of enforcement action",
					JSONPath:    ".spec.enforcementAction",
					Type:        "string",
				},
				{
					Name:        "total-violations",
					Description: "Total number of violations",
					JSONPath:    ".status.totalViolations",
					Type:        "integer",
				},
			},
		},
	}

	// Defaulting functions are not found in versionless CRD package
	crdv1 := &apiextensionsv1.CustomResourceDefinition{}
	if err := scheme.Convert(crd, crdv1, nil); err != nil {
		return nil, err
	}
	scheme.Default(crdv1)

	crd2 := &apiextensions.CustomResourceDefinition{}
	if err := scheme.Convert(crdv1, crd2, nil); err != nil {
		return nil, err
	}
	crd2.ObjectMeta.Name = fmt.Sprintf("%s.%s", crd.Spec.Names.Plural, constraints.Group)

	labels := templ.ObjectMeta.Labels
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["gatekeeper.sh/constraint"] = "yes"
	crd2.ObjectMeta.Labels = labels

	return crd2, nil
}
