package crds

import (
	"fmt"
	"strings"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	clienterrors "github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsvalidation "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/validation"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apivalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidateTargets ensures that the targets field has the appropriate values.
func ValidateTargets(templ *templates.ConstraintTemplate) error {
	targets := templ.Spec.Targets
	if targets == nil {
		return fmt.Errorf(`%w: field "targets" not specified in ConstraintTemplate spec`,
			clienterrors.ErrInvalidConstraintTemplate)
	}

	switch len(targets) {
	case 0:
		return fmt.Errorf("%w: no targets specified: ConstraintTemplate must specify one target",
			clienterrors.ErrInvalidConstraintTemplate)
	case 1:
		return nil
	default:
		return fmt.Errorf("%w: multi-target templates are not currently supported",
			clienterrors.ErrInvalidConstraintTemplate)
	}
}

// ValidateCRD calls the CRD package's validation on an internal representation of the CRD.
func ValidateCRD(crd *apiextensions.CustomResourceDefinition) error {
	errs := apiextensionsvalidation.ValidateCustomResourceDefinition(crd, apiextensionsv1.SchemeGroupVersion)
	if len(errs) > 0 {
		return errs.ToAggregate()
	}
	return nil
}

// ValidateCR validates the provided custom resource against its CustomResourceDefinition.
func ValidateCR(cr *unstructured.Unstructured, crd *apiextensions.CustomResourceDefinition) error {
	validator, _, err := validation.NewSchemaValidator(crd.Spec.Validation)
	if err != nil {
		return err
	}
	if err := validation.ValidateCustomResource(field.NewPath(""), cr, validator); err != nil {
		return err.ToAggregate()
	}

	if errs := apivalidation.IsDNS1123Subdomain(cr.GetName()); len(errs) != 0 {
		return fmt.Errorf("%w: invalid name: %q",
			ErrInvalidConstraint, strings.Join(errs, "\n"))
	}

	if cr.GetKind() != crd.Spec.Names.Kind {
		return fmt.Errorf("%w: wrong kind %q for constraint %q; want %q",
			ErrInvalidConstraint, cr.GetName(), cr.GetKind(), crd.Spec.Names.Kind)
	}

	if cr.GroupVersionKind().Group != constraints.Group {
		return fmt.Errorf("%w: unsupported group %q for constraint %q; allowed group: %q",
			ErrInvalidConstraint, cr.GetName(), cr.GroupVersionKind().Group, constraints.Group)
	}

	if !supportedVersions[cr.GroupVersionKind().Version] {
		return fmt.Errorf("%w: unsupported version %q for Constraint %q; supported versions: %v",
			ErrInvalidConstraint, cr.GroupVersionKind().Version, cr.GetName(), supportedVersions)
	}
	return nil
}
