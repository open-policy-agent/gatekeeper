package v1alpha1

import (
	apisTemplates "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
)

// ToVersionless runs defaulting functions and then converts the ConstraintTemplate to the
// versionless api representation
func (versioned *ConstraintTemplate) ToVersionless() (*templates.ConstraintTemplate, error) {
	if err := AddToScheme(apisTemplates.Scheme); err != nil {
		return nil, err
	}

	versionedCopy := versioned.DeepCopy()
	apisTemplates.Scheme.Default(versionedCopy)

	versionless := &templates.ConstraintTemplate{}
	if err := apisTemplates.Scheme.Convert(versionedCopy, versionless, nil); err != nil {
		return nil, err
	}

	return versionless, nil
}
