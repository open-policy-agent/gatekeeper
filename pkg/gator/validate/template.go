package validate

import (
	"encoding/json"
	"fmt"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis"
	templatesv1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/gatekeeper/pkg/gator"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

var scheme *runtime.Scheme

func init() {
	scheme = runtime.NewScheme()
	apis.AddToScheme(scheme)
}

// this logic is copied from Will's code.  Once I get this working, let's
// figure out how to refactor this code into a common location.

type versionless interface {
	ToVersionless() (*templates.ConstraintTemplate, error)
}

func unstructToTemplate(u *unstructured.Unstructured) (*templates.ConstraintTemplate, error) {
	gvk := u.GroupVersionKind()
	if gvk.Group != templatesv1.SchemeGroupVersion.Group || gvk.Kind != "ConstraintTemplate" {
		return nil, fmt.Errorf("%w", gator.ErrNotATemplate)
	}

	t, err := scheme.New(gvk)
	if err != nil {
		// The type isn't registered in the scheme.
		return nil, fmt.Errorf("%w: %v", gator.ErrAddingTemplate, err)
	}

	// YAML parsing doesn't properly handle ObjectMeta, so we must
	// marshal/unmashal through JSON.
	jsonBytes, err := u.MarshalJSON()
	if err != nil {
		// Indicates a bug in unstructured.MarshalJSON(). Any Unstructured
		// unmarshalled from YAML should be marshallable to JSON.
		return nil, fmt.Errorf("calling unstructured.MarshalJSON(): %w", err)
	}
	err = json.Unmarshal(jsonBytes, t)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", gator.ErrAddingTemplate, err)
	}

	v, isVersionless := t.(versionless)
	if !isVersionless {
		return nil, fmt.Errorf("%w: %T", gator.ErrConvertingTemplate, t)
	}

	template, err := v.ToVersionless()
	if err != nil {
		// This shouldn't happen unless there's a bug in the conversion functions.
		// Most likely it means the conversion functions weren't generated.
		return nil, fmt.Errorf("%w: %v", gator.ErrConvertingTemplate, err)
	}

	return template, nil
}
