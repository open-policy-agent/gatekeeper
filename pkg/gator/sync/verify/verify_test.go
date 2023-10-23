package verify

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/fixtures"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/reader"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestVerify(t *testing.T) {
	tcs := []struct {
		name      string
		inputs    []string
		discovery string
		wantReqs  map[string][]int
		wantErrs  map[string]error
		err       error
	}{
		{
			name: "basic req unfulfilled",
			inputs: []string{
				fixtures.TemplateReferential,
			},
			wantReqs: map[string][]int{
				"k8suniqueserviceselector": {1},
			},
			wantErrs: map[string]error{},
		},
		{
			name: "one template has error, one has basic req unfulfilled",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.TemplateReferentialBadAnnotation,
			},
			wantReqs: map[string][]int{
				"k8suniqueserviceselector": {1},
			},
			wantErrs: map[string]error{
				"k8suniqueingresshostbadannotation": fmt.Errorf("json: cannot unmarshal object into Go value of type parser.CompactSyncRequirements"),
			},
		},
		{
			name: "basic req fulfilled by syncset",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.Config,
			},
			wantReqs: map[string][]int{},
			wantErrs: map[string]error{},
		},
		{
			name: "basic req fulfilled by syncset and discoveryresults",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.Config,
			},
			discovery: `{"": {"v1": ["Service"]}}`,
			wantReqs:  map[string][]int{},
			wantErrs:  map[string]error{},
		},
		{
			name: "basic req fulfilled by syncset but not discoveryresults",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.Config,
			},
			discovery: `{"extensions": {"v1beta1": ["Ingress"]}}`,
			wantReqs: map[string][]int{
				"k8suniqueserviceselector": {1},
			},
			wantErrs: map[string]error{},
		},
		{
			name: "multi equivalentset req fulfilled by syncset",
			inputs: []string{
				fixtures.TemplateReferentialMultEquivSets,
				fixtures.SyncSet,
			},
			wantReqs: map[string][]int{},
			wantErrs: map[string]error{},
		},
		{
			name: "multi requirement, one req fulfilled by syncset",
			inputs: []string{
				fixtures.TemplateReferentialMultReqs,
				fixtures.SyncSet,
			},
			wantReqs: map[string][]int{
				"k8suniqueingresshostmultireq": {2},
			},
			wantErrs: map[string]error{},
		},
		{
			name: "multiple templates, syncset and config",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.TemplateReferentialMultEquivSets,
				fixtures.TemplateReferentialMultReqs,
				fixtures.Config,
				fixtures.SyncSet,
			},
			wantReqs: map[string][]int{
				"k8suniqueingresshostmultireq": {2},
			},
			wantErrs: map[string]error{},
		},
		{
			name:     "no data of any kind",
			inputs:   []string{},
			wantReqs: map[string][]int{},
			wantErrs: map[string]error{},
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

			gotReqs, gotErrs, err := Verify(objs, tc.discovery)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else if err != nil {
				require.NoError(t, err)
			}

			if diff := cmp.Diff(tc.wantReqs, gotReqs); diff != "" {
				t.Errorf("diff in missingRequirements objects (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.wantErrs, gotErrs); diff != "" {
				t.Errorf("diff in templateErrs objects (-want +got):\n%s", diff)
			}
		})
	}
}
