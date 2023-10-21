package verify

import (
	"fmt"

	cfapis "github.com/open-policy-agent/frameworks/constraint/pkg/apis"
	templatesv1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	gkapis "github.com/open-policy-agent/gatekeeper/v3/apis"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager/parser"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/reader"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var scheme *runtime.Scheme

func init() {
	scheme = runtime.NewScheme()
	err := cfapis.AddToScheme(scheme)
	if err != nil {
		panic(err)
	}
	err = gkapis.AddToScheme(scheme)
	if err != nil {
		panic(err)
	}
}

func Verify(unstrucs []*unstructured.Unstructured, flagDiscoveryResults string) (map[string][]int, error) {

	discoveryResults, err := reader.ReadDiscoveryResults(flagDiscoveryResults)
	if err != nil {
		return nil, fmt.Errorf("reading: %v", err)
	}

	templates := []*templates.ConstraintTemplate{}
	syncedGVKs := map[schema.GroupVersionKind]struct{}{}

	for _, obj := range unstrucs {
		if isSyncSet(obj) {
			syncSet, err := reader.ToSyncSet(scheme, obj)
			if err != nil {
				return nil, fmt.Errorf("converting unstructured %q to syncset: %w", obj.GetName(), err)
			}
			for _, gvkEntry := range syncSet.Spec.GVKs {
				gvk := schema.GroupVersionKind{
					Group:   gvkEntry.Group,
					Version: gvkEntry.Version,
					Kind:    gvkEntry.Kind,
				}
				if _, exists := discoveryResults[gvk]; exists || discoveryResults == nil {
					syncedGVKs[gvk] = struct{}{}
				}
			}
		}
		if isConfig(obj) {
			config, err := reader.ToConfig(scheme, obj)
			if err != nil {
				return nil, fmt.Errorf("converting unstructured %q to config: %w", obj.GetName(), err)
			}
			for _, syncOnlyEntry := range config.Spec.Sync.SyncOnly {
				gvk := schema.GroupVersionKind{
					Group:   syncOnlyEntry.Group,
					Version: syncOnlyEntry.Version,
					Kind:    syncOnlyEntry.Kind,
				}
				if _, exists := discoveryResults[gvk]; exists || discoveryResults == nil {
					syncedGVKs[gvk] = struct{}{}
				}
			}
		}
		if isTemplate(obj) {
			templ, err := reader.ToTemplate(scheme, obj)
			if err != nil {
				return nil, fmt.Errorf("converting unstructured %q to template: %w", obj.GetName(), err)
			}
			templates = append(templates, templ)
		}
	}

	missingReqs := map[string][]int{}

	for _, templ := range templates {
		// Fetch syncrequirements from template
		syncRequirements, err := parser.ReadSyncRequirements(templ)
		if err != nil {
			return nil, fmt.Errorf("reading sync requirements from template %q: %w", templ.GetName(), err)
		}
		for i, requirement := range syncRequirements {
			requirementMet := false
			for gvk := range requirement {
				if _, exists := syncedGVKs[gvk]; exists {
					requirementMet = true
				}
			}
			if !requirementMet {
				missingReqs[templ.Name] = append(missingReqs[templ.Name], i+1)
			}
		}
	}
	return missingReqs, nil
}

func isTemplate(u *unstructured.Unstructured) bool {
	gvk := u.GroupVersionKind()
	return gvk.Group == templatesv1.SchemeGroupVersion.Group && gvk.Kind == "ConstraintTemplate"
}

func isConfig(u *unstructured.Unstructured) bool {
	gvk := u.GroupVersionKind()
	return gvk.Group == "config.gatekeeper.sh" && gvk.Kind == "Config"
}

func isSyncSet(u *unstructured.Unstructured) bool {
	gvk := u.GroupVersionKind()
	return gvk.Group == "syncset.gatekeeper.sh" && gvk.Kind == "SyncSet"
}
