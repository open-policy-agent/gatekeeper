package test

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis"
	templatesv1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/pkg/gator"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
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

func Test(objs []*unstructured.Unstructured) (*types.Responses, error) {
	// create the client

	driver, err := local.New(local.Tracing(false))
	if err != nil {
		return nil, err
	}

	client, err := constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver))
	if err != nil {
		return nil, fmt.Errorf("creating OPA client: %w", err)
	}

	// search for templates, add them if they exist
	ctx := context.Background()
	for _, obj := range objs {
		if !isTemplate(obj) {
			continue
		}

		templ, err := gator.ToTemplate(scheme, obj)
		if err != nil {
			return nil, fmt.Errorf("converting unstructured %q to template: %w", obj.GetName(), err)
		}

		_, err = client.AddTemplate(ctx, templ)
		if err != nil {
			return nil, fmt.Errorf("adding template %q: %w", templ.GetName(), err)
		}
	}

	// add all constraints.  A constraint must be added after its associated
	// template or OPA will return an error
	for _, obj := range objs {
		if !isConstraint(obj) {
			continue
		}

		_, err := client.AddConstraint(context.Background(), obj)
		if err != nil {
			return nil, fmt.Errorf("adding constraint %q: %w", obj.GetName(), err)
		}
	}

	// finally, add all the data.
	for _, obj := range objs {
		_, err := client.AddData(ctx, obj)
		if err != nil {
			return nil, fmt.Errorf("adding data of GVK %q: %w", obj.GroupVersionKind().String(), err)
		}
	}

	// now audit all objects
	responses := &types.Responses{
		ByTarget: make(map[string]*types.Response),
	}
	for _, obj := range objs {
		review, err := client.Review(ctx, obj)
		if err != nil {
			return nil, fmt.Errorf("reviewing %v %s/%s: %v",
				obj.GroupVersionKind(), obj.GetNamespace(), obj.GetName(), err)
		}

		for targetName, r := range review.ByTarget {
			targetResponse := responses.ByTarget[targetName]
			if targetResponse == nil {
				targetResponse = &types.Response{}
				targetResponse.Target = targetName
			}

			targetResponse.Results = append(targetResponse.Results, r.Results...)

			if r.Trace != nil {
				var trace string
				if targetResponse.Trace != nil {
					trace = *targetResponse.Trace
				}

				trace = trace + "\n\n" + *r.Trace
				targetResponse.Trace = &trace
			}

			responses.ByTarget[targetName] = targetResponse
		}
	}

	return responses, nil
}

func isTemplate(u *unstructured.Unstructured) bool {
	gvk := u.GroupVersionKind()
	return gvk.Group == templatesv1.SchemeGroupVersion.Group && gvk.Kind == "ConstraintTemplate"
}

func isConstraint(u *unstructured.Unstructured) bool {
	gvk := u.GroupVersionKind()
	return gvk.Group == "constraints.gatekeeper.sh"
}
