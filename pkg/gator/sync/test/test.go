package test

import (
	"fmt"

	cfapis "github.com/open-policy-agent/frameworks/constraint/pkg/apis"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	gkapis "github.com/open-policy-agent/gatekeeper/v3/apis"
	gvkmanifestv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/gvkmanifest/v1alpha1"
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
// outputs a set of missing sync requirements per template and ingestion problems per template.
func Test(unstrucs []*unstructured.Unstructured, omitGVKManifest bool) (map[string]parser.SyncRequirements, map[string]error, error) {
	templates := map[*templates.ConstraintTemplate]parser.SyncRequirements{}
	syncedGVKs := map[schema.GroupVersionKind]struct{}{}
	templateErrs := map[string]error{}
	hasConfig := false
	var gvkManifest *gvkmanifestv1alpha1.GVKManifest
	var err error

	for _, obj := range unstrucs {
		switch {
		case reader.IsSyncSet(obj):
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
				syncedGVKs[gvk] = struct{}{}
			}
		case reader.IsConfig(obj):
			if hasConfig {
				return nil, nil, fmt.Errorf("multiple configs found; Config is a singleton resource")
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
				syncedGVKs[gvk] = struct{}{}
			}
		case reader.IsTemplate(obj):
			templ, err := reader.ToTemplate(scheme, obj)
			if err != nil {
				templateErrs[obj.GetName()] = err
				continue
			}
			syncRequirements, err := parser.ReadSyncRequirements(templ)
			if err != nil {
				templateErrs[templ.GetName()] = err
				continue
			}
			templates[templ] = syncRequirements
		case reader.IsGVKManifest(obj):
			if gvkManifest == nil {
				gvkManifest, err = reader.ToGVKManifest(scheme, obj)
				if err != nil {
					return nil, nil, fmt.Errorf("converting unstructured %q to gvkmanifest: %w", obj.GetName(), err)
				}
			} else {
				return nil, nil, fmt.Errorf("multiple GVK manifests found; please provide one manifest enumerating the GVKs supported by the cluster")
			}
		default:
			fmt.Printf("skipping unstructured %q because it is not a syncset, config, gvk manifest, or template\n", obj.GetName())
		}
	}

	// Don't assess requirement fulfillment if there was an error parsing any of the templates.
	if len(templateErrs) != 0 {
		return nil, templateErrs, nil
	}

	// Crosscheck synced gvks with supported gvks.
	if gvkManifest == nil {
		if !omitGVKManifest {
			return nil, nil, fmt.Errorf("no GVK manifest found; please provide a manifest enumerating the GVKs supported by the cluster")
		}
		fmt.Print("ignoring absence of supported GVK manifest due to --force-omit-gvk-manifest flag; will assume all synced GVKs are supported by cluster\n")
	} else {
		supportedGVKs := map[schema.GroupVersionKind]struct{}{}
		for group, versions := range gvkManifest.Spec.Groups {
			for version, kinds := range versions {
				for _, kind := range kinds {
					gvk := schema.GroupVersionKind{
						Group:   group,
						Version: version,
						Kind:    kind,
					}
					supportedGVKs[gvk] = struct{}{}
				}
			}
		}
		for gvk := range syncedGVKs {
			if _, exists := supportedGVKs[gvk]; !exists {
				delete(syncedGVKs, gvk)
			}
		}
	}

	missingReqs := map[string]parser.SyncRequirements{}

	for templ, reqs := range templates {
		// Fetch syncrequirements from template
		for _, requirement := range reqs {
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
	return missingReqs, nil, nil
}
