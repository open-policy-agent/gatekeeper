package test

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis"
	templatesv1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	"github.com/open-policy-agent/gatekeeper/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/pkg/gator"
	mutationtypes "github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
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

func Test(objs []*unstructured.Unstructured, includeTrace bool) (*GatorResponses, error) {
	// create the client

	driver, err := local.New(local.Tracing(includeTrace))
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

	// create the expander
	er, err := gator.NewExpander(objs)
	if err != nil {
		return nil, fmt.Errorf("error creating expander: %s", err)
	}

	// now audit all objects
	responses := &GatorResponses{
		ByTarget: make(map[string]*GatorResponse),
	}
	for _, obj := range objs {
		// Try to attach the namespace if it was supplied (ns will be nil otherwise)
		ns, _ := er.NamespaceForResource(obj)
		au := target.AugmentedUnstructured{
			Object:    *obj,
			Namespace: ns,
			Source:    mutationtypes.SourceTypeOriginal,
		}

		review, err := client.Review(ctx, au)
		if err != nil {
			return nil, fmt.Errorf("reviewing %v %s/%s: %v",
				obj.GroupVersionKind(), obj.GetNamespace(), obj.GetName(), err)
		}

		// Attempt to expand the obj and review resultant resources (if any)
		resultants, err := er.Expand(obj)
		if err != nil {
			return nil, fmt.Errorf("expanding resource %s: %s", obj.GetName(), err)
		}
		for _, resultant := range resultants {
			au := target.AugmentedUnstructured{
				Object:    *resultant.Obj,
				Namespace: ns,
				Source:    mutationtypes.SourceTypeGenerated,
			}
			resultantReview, err := client.Review(ctx, au)
			if err != nil {
				return nil, fmt.Errorf("reviewing expanded resource %v %s/%s: %v",
					resultant.Obj.GroupVersionKind(), resultant.Obj.GetNamespace(), resultant.Obj.GetName(), err)
			}
			expansion.OverrideEnforcementAction(resultant.EnforcementAction, resultantReview)
			expansion.AggregateResponses(resultant.TemplateName, review, resultantReview)
		}

		for targetName, r := range review.ByTarget {
			targetResponse := responses.ByTarget[targetName]
			if targetResponse == nil {
				targetResponse = &GatorResponse{}
				targetResponse.Target = targetName
			}

			// convert framework results to gator results, which contain a
			// reference to the violating resource
			gResults := make([]*GatorResult, len(r.Results))
			for i, r := range r.Results {
				gResults[i] = fromFrameworkResult(r, obj)
			}
			targetResponse.Results = append(targetResponse.Results, gResults...)

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
