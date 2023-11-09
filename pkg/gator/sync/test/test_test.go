package test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager/parser"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/fixtures"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/reader"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestTest(t *testing.T) {
	tcs := []struct {
		name         string
		inputs       []string
		omitManifest bool
		wantReqs     map[string]parser.SyncRequirements
		wantErrs     map[string]error
		err          error
	}{
		// {
		// 	name: "basic req unfulfilled",
		// 	inputs: []string{
		// 		fixtures.TemplateReferential,
		// 	},
		// 	omitManifest: true,
		// 	wantReqs: map[string]parser.SyncRequirements{
		// 		"k8suniqueserviceselector": {
		// 			parser.GVKEquivalenceSet{
		// 				{
		// 					Group:   "",
		// 					Version: "v1",
		// 					Kind:    "Service",
		// 				}: struct{}{},
		// 			},
		// 		},
		// 	},
		// 	wantErrs: map[string]error{},
		// },
		// {
		// 	name: "one template having error stops requirement evaluation",
		// 	inputs: []string{
		// 		fixtures.TemplateReferential,
		// 		fixtures.TemplateReferentialBadAnnotation,
		// 	},
		// 	omitManifest: true,
		// 	wantReqs:     nil,
		// 	wantErrs: map[string]error{
		// 		"k8suniqueingresshostbadannotation": fmt.Errorf("json: cannot unmarshal object into Go value of type parser.CompactSyncRequirements"),
		// 	},
		// },
		// {
		// 	name: "basic req fulfilled by syncset",
		// 	inputs: []string{
		// 		fixtures.TemplateReferential,
		// 		fixtures.Config,
		// 	},
		// 	omitManifest: true,
		// 	wantReqs:     map[string]parser.SyncRequirements{},
		// 	wantErrs:     map[string]error{},
		// },
		{
			name: "basic req fulfilled by syncset and supported by cluster",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.Config,
				fixtures.GVKManifest,
			},
			wantReqs: map[string]parser.SyncRequirements{},
			wantErrs: map[string]error{},
		},
		// {
		// 	name: "basic req fulfilled by syncset but not supported by cluster",
		// 	inputs: []string{
		// 		fixtures.TemplateReferential,
		// 		fixtures.Config,
		// 		fixtures.GVKManifest,
		// 	},
		// 	wantReqs: map[string]parser.SyncRequirements{
		// 		"k8suniqueserviceselector": {
		// 			parser.GVKEquivalenceSet{
		// 				{
		// 					Group:   "",
		// 					Version: "v1",
		// 					Kind:    "Service",
		// 				}: struct{}{},
		// 			},
		// 		},
		// 	},
		// 	wantErrs: map[string]error{},
		// },
		// {
		// 	name: "multi equivalentset req fulfilled by syncset",
		// 	inputs: []string{
		// 		fixtures.TemplateReferentialMultEquivSets,
		// 		fixtures.SyncSet,
		// 	},
		// 	omitManifest: true,
		// 	wantReqs:     map[string]parser.SyncRequirements{},
		// 	wantErrs:     map[string]error{},
		// },
		// {
		// 	name: "multi requirement, one req fulfilled by syncset",
		// 	inputs: []string{
		// 		fixtures.TemplateReferentialMultReqs,
		// 		fixtures.SyncSet,
		// 	},
		// 	omitManifest: true,
		// 	wantReqs: map[string]parser.SyncRequirements{
		// 		"k8suniqueingresshostmultireq": {
		// 			parser.GVKEquivalenceSet{
		// 				{
		// 					Group:   "",
		// 					Version: "v1",
		// 					Kind:    "Pod",
		// 				}: struct{}{},
		// 			},
		// 		},
		// 	},
		// 	wantErrs: map[string]error{},
		// },
		// {
		// 	name: "multiple templates, syncset and config",
		// 	inputs: []string{
		// 		fixtures.TemplateReferential,
		// 		fixtures.TemplateReferentialMultEquivSets,
		// 		fixtures.TemplateReferentialMultReqs,
		// 		fixtures.Config,
		// 		fixtures.SyncSet,
		// 	},
		// 	omitManifest: true,
		// 	wantReqs: map[string]parser.SyncRequirements{
		// 		"k8suniqueingresshostmultireq": {
		// 			parser.GVKEquivalenceSet{
		// 				{
		// 					Group:   "",
		// 					Version: "v1",
		// 					Kind:    "Pod",
		// 				}: struct{}{},
		// 			},
		// 		},
		// 	},
		// 	wantErrs: map[string]error{},
		// },
		// {
		// 	name:         "no data of any kind",
		// 	inputs:       []string{},
		// 	omitManifest: true,
		// 	wantReqs:     map[string]parser.SyncRequirements{},
		// 	wantErrs:     map[string]error{},
		// },
		// {
		// 	name: "error if manifest not provided and omitGVKManifest not set",
		// 	inputs: []string{
		// 		fixtures.TemplateReferential,
		// 		fixtures.Config,
		// 	},
		// 	wantReqs: map[string]parser.SyncRequirements{},
		// 	wantErrs: map[string]error{},
		// 	err:      fmt.Errorf("no GVK manifest found; please provide a manifest enumerating the GVKs supported by the cluster"),
		// },
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// convert the test resources to unstructureds
			var objs []*unstructured.Unstructured
			for _, input := range tc.inputs {
				u, err := reader.ReadUnstructured([]byte(input))
				require.NoError(t, err)
				objs = append(objs, u)
			}

			gotReqs, gotErrs, err := Test(objs, tc.omitManifest)
			if tc.err != nil {
				if tc.err.Error() != err.Error() {
					t.Errorf("error mismatch: want %v, got %v", tc.err, err)
				}
			} else if err != nil {
				require.NoError(t, err)
			}

			if diff := cmp.Diff(tc.wantReqs, gotReqs); diff != "" {
				t.Errorf("diff in missingRequirements objects (-want +got):\n%s", diff)
			}

			for key, wantErr := range tc.wantErrs {
				if gotErr, ok := gotErrs[key]; ok {
					if wantErr.Error() != gotErr.Error() {
						t.Errorf("error mismatch for %s: want %v, got %v", key, wantErr, gotErr)
					}
				} else {
					t.Errorf("missing error for %s", key)
				}
			}
			for key, gotErr := range gotErrs {
				if _, ok := tc.wantErrs[key]; !ok {
					t.Errorf("unexpected error for %s: %v", key, gotErr)
				}
			}
		})
	}
}
