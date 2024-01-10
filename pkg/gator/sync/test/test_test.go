package test

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager/parser"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/fixtures"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/reader"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

func TestTest(t *testing.T) {
	tcs := []struct {
		name         string
		inputs       []string
		omitManifest bool
		wantReqs     map[string]parser.SyncRequirements
	}{
		{
			name: "basic req unfulfilled",
			inputs: []string{
				fixtures.TemplateReferential,
			},
			omitManifest: true,
			wantReqs: map[string]parser.SyncRequirements{
				"k8suniqueserviceselector": {
					parser.GVKEquivalenceSet{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Service",
						}: struct{}{},
					},
				},
			},
		},
		{
			name: "basic req fulfilled by config",
			inputs: []string{
				fixtures.TemplateReferential,
				toYAMLString(fakes.ConfigFor([]schema.GroupVersionKind{
					{
						Group:   "",
						Version: "v1",
						Kind:    "Service",
					},
					{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
				})),
			},
			omitManifest: true,
			wantReqs:     map[string]parser.SyncRequirements{},
		},
		{
			name: "basic req fulfilled by config and supported by cluster",
			inputs: []string{
				fixtures.TemplateReferential,
				toYAMLString(fakes.ConfigFor([]schema.GroupVersionKind{
					{
						Group:   "",
						Version: "v1",
						Kind:    "Service",
					},
					{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
				})),
				toYAMLString(fakes.GVKManifestFor("gvkmanifest", []schema.GroupVersionKind{
					{
						Group:   "",
						Version: "v1",
						Kind:    "Service",
					},
				})),
			},
			wantReqs: map[string]parser.SyncRequirements{},
		},
		{
			name: "basic req fulfilled by config but not supported by cluster",
			inputs: []string{
				fixtures.TemplateReferential,
				toYAMLString(fakes.ConfigFor([]schema.GroupVersionKind{
					{
						Group:   "",
						Version: "v1",
						Kind:    "Service",
					},
					{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
				})),
				toYAMLString(fakes.GVKManifestFor("gvkmanifest", []schema.GroupVersionKind{
					{
						Group:   "app",
						Version: "v1",
						Kind:    "Deployment",
					},
				})),
			},
			wantReqs: map[string]parser.SyncRequirements{
				"k8suniqueserviceselector": {
					parser.GVKEquivalenceSet{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Service",
						}: struct{}{},
					},
				},
			},
		},
		{
			name: "multi equivalentset req fulfilled by syncset",
			inputs: []string{
				fixtures.TemplateReferentialMultEquivSets,
				toYAMLString(fakes.SyncSetFor("syncset", []schema.GroupVersionKind{
					{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
					{
						Group:   "networking.k8s.io",
						Version: "v1",
						Kind:    "Ingress",
					},
				})),
			},
			omitManifest: true,
			wantReqs:     map[string]parser.SyncRequirements{},
		},
		{
			name: "multi requirement, one req fulfilled by syncset",
			inputs: []string{
				fixtures.TemplateReferentialMultReqs,
				toYAMLString(fakes.SyncSetFor("syncset", []schema.GroupVersionKind{
					{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
					{
						Group:   "networking.k8s.io",
						Version: "v1",
						Kind:    "Ingress",
					},
				}))},
			omitManifest: true,
			wantReqs: map[string]parser.SyncRequirements{
				"k8suniqueingresshostmultireq": {
					parser.GVKEquivalenceSet{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Pod",
						}: struct{}{},
					},
				},
			},
		},
		{
			name: "multiple templates, syncset and config",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.TemplateReferentialMultEquivSets,
				fixtures.TemplateReferentialMultReqs,
				toYAMLString(fakes.ConfigFor([]schema.GroupVersionKind{
					{
						Group:   "",
						Version: "v1",
						Kind:    "Service",
					},
					{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
				})),
				toYAMLString(fakes.SyncSetFor("syncset", []schema.GroupVersionKind{
					{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
					{
						Group:   "networking.k8s.io",
						Version: "v1",
						Kind:    "Ingress",
					},
				})),
			},
			omitManifest: true,
			wantReqs: map[string]parser.SyncRequirements{
				"k8suniqueingresshostmultireq": {
					parser.GVKEquivalenceSet{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Pod",
						}: struct{}{},
					},
				},
			},
		},
		{
			name:         "no data of any kind",
			inputs:       []string{},
			omitManifest: true,
			wantReqs:     map[string]parser.SyncRequirements{},
		},
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

			require.NoError(t, err)

			if gotErrs != nil {
				t.Errorf("got unexpected errors: %v", gotErrs)
			}

			if diff := cmp.Diff(tc.wantReqs, gotReqs); diff != "" {
				t.Errorf("diff in missingRequirements objects (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTest_Errors(t *testing.T) {
	tcs := []struct {
		name         string
		inputs       []string
		omitManifest bool
		wantErrs     map[string]error
		err          error
	}{
		{
			name: "one template having error stops requirement evaluation",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.TemplateReferentialBadAnnotation,
			},
			omitManifest: true,
			wantErrs: map[string]error{
				"k8suniqueingresshostbadannotation": fmt.Errorf("json: cannot unmarshal object into Go value of type parser.CompactSyncRequirements"),
			},
		},
		{
			name: "error if manifest not provided and omitGVKManifest not set",
			inputs: []string{
				fixtures.TemplateReferential,
				toYAMLString(fakes.ConfigFor([]schema.GroupVersionKind{
					{
						Group:   "",
						Version: "v1",
						Kind:    "Service",
					},
					{
						Group:   "apps",
						Version: "v1",
						Kind:    "Deployment",
					},
				})),
			},
			wantErrs: map[string]error{},
			err:      fmt.Errorf("no GVK manifest found; please provide a manifest enumerating the GVKs supported by the cluster"),
		},
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

			if gotReqs != nil {
				t.Errorf("got unexpected requirements: %v", gotReqs)
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
		})
	}
}

func toYAMLString(obj runtime.Object) string {
	yaml, _ := yaml.Marshal(obj)
	return string(yaml)
}
