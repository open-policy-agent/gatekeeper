package validate

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis"
	templatesv1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/pkg/gator"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

var scheme *runtime.Scheme

func init() {
	scheme = runtime.NewScheme()
	err := apis.AddToScheme(scheme)
	if err != nil {
		panic(err)
	}
}

func Validate(objs []*unstructured.Unstructured) (*types.Responses, error) {
	// create the client
	client, err := gator.NewOPAClient()
	if err != nil {
		return nil, fmt.Errorf("creating OPA client: %w", err)
	}

	// search for templates, add them if they exist
	hasTemplates := false
	for _, obj := range objs {
		if !isTemplate(obj) {
			continue
		}
		hasTemplates = true

		templ, err := gator.ToTemplate(scheme, obj)
		if err != nil {
			return nil, fmt.Errorf("converting unstructured %q to template: %w", obj.GetName(), err)
		}

		_, err = client.AddTemplate(templ)
		if err != nil {
			return nil, fmt.Errorf("adding template %q: %w", templ.GetName(), err)
		}
	}
	if !hasTemplates {
		return nil, fmt.Errorf("must included templates in Validate input")
	}

	// add all constraints.  A constraint must be added after its associated
	// template or OPA will return an error
	hasConstraints := false
	for _, obj := range objs {
		if !isConstraint(obj) {
			continue
		}

		hasConstraints = true

		_, err := client.AddConstraint(context.Background(), obj)
		if err != nil {
			return nil, fmt.Errorf("adding constraint %q: %w", obj.GetName(), err)
		}
	}
	if !hasConstraints {
		return nil, fmt.Errorf("must included constraints in Validate input")
	}

	// finally, add all the data.  Filter out templates and constraints, as we
	// can't write policy about those.
	for _, obj := range objs {
		if isTemplate(obj) || isConstraint(obj) {
			continue
		}

		_, err := client.AddData(context.Background(), obj)
		if err != nil {
			return nil, fmt.Errorf("adding data of GVK %q: %w", obj.GroupVersionKind().String(), err)
		}
	}

	return client.Audit(context.Background())
}

func isTemplate(u *unstructured.Unstructured) bool {
	gvk := u.GroupVersionKind()
	return gvk.Group == templatesv1.SchemeGroupVersion.Group && gvk.Kind == "ConstraintTemplate"
}

func isConstraint(u *unstructured.Unstructured) bool {
	gvk := u.GroupVersionKind()
	return gvk.Group == "constraints.gatekeeper.sh"
}
