package verify

import (
	"fmt"

	cfapis "github.com/open-policy-agent/frameworks/constraint/pkg/apis"
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

// Reads a list of unstructured objects and a string containing supported GVKs and
// outputs a set of missing sync requirements per template and ingestion problems per template
func Verify(unstrucs []*unstructured.Unstructured, flagSupportedGVKs string) (map[string]parser.SyncRequirements, map[string]error, error) {
	discoveryResults, err := reader.ReadDiscoveryResults(flagSupportedGVKs)
	if err != nil {
		return nil, nil, fmt.Errorf("reading: %w", err)
	}

	templates := []*templates.ConstraintTemplate{}
	syncedGVKs := map[schema.GroupVersionKind]struct{}{}
	templateErrs := map[string]error{}
	hasConfig := false

	for _, obj := range unstrucs {
		if reader.IsSyncSet(obj) {
			syncSet, err := reader.ToSyncSet(scheme, obj)
			if err != nil {
				return nil, nil, fmt.Errorf("converting unstructured %q to syncset: %w", obj.GetName(), err)
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
		} else if reader.IsConfig(obj) {
			if hasConfig {
				return nil, nil, fmt.Errorf("Multiple configs found. Config is a singleton resource.")
			}
			config, err := reader.ToConfig(scheme, obj)
			if err != nil {
				return nil, nil, fmt.Errorf("converting unstructured %q to config: %w", obj.GetName(), err)
			}
			hasConfig = true
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
		} else if reader.IsTemplate(obj) {
			templ, err := reader.ToTemplate(scheme, obj)
			if err != nil {
				templateErrs[obj.GetName()] = err
				continue
			}
			templates = append(templates, templ)
		} else {
			fmt.Printf("Skipping unstructured %q because it is not a syncset, config, or template\n", obj.GetName())
		}
	}

	missingReqs := map[string]parser.SyncRequirements{}

	for _, templ := range templates {
		// Fetch syncrequirements from template
		syncRequirements, err := parser.ReadSyncRequirements(templ)
		if err != nil {
			templateErrs[templ.GetName()] = err
			continue
		}
		for _, requirement := range syncRequirements {
			requirementMet := false
			for gvk := range requirement {
				if _, exists := syncedGVKs[gvk]; exists {
					requirementMet = true
				}
			}
			if !requirementMet {
				missingReqs[templ.Name] = append(missingReqs[templ.Name], requirement)
			}
		}
	}
	return missingReqs, templateErrs, nil
}
