package verify

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/fixtures"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestVerify(t *testing.T) {
	tcs := []struct {
		name      string
		inputs    []string
		discovery string
		want      map[string][]int
		err       error
	}{
		{
			name: "basic req unfulfilled",
			inputs: []string{
				fixtures.TemplateReferential,
			},
			want: map[string][]int{
				"k8suniqueserviceselector": {1},
			},
		},
		{
			name: "basic req fulfilled by syncset",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.Config,
			},
			want: map[string][]int{},
		},
		{
			name: "basic req fulfilled by syncset and discoveryresults",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.Config,
			},
			discovery: `{"": {"v1": ["Service"]}}`,
			want:      map[string][]int{},
		},
		{
			name: "basic req fulfilled by syncset but not discoveryresults",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.Config,
			},
			discovery: `{"extensions": {"v1beta1": ["Ingress"]}}`,
			want: map[string][]int{
				"k8suniqueserviceselector": {1},
			},
		},
		{
			name: "multi equivalentset req fulfilled by syncset",
			inputs: []string{
				fixtures.TemplateReferentialMultEquivSets,
				fixtures.SyncSet,
			},
			want: map[string][]int{},
		},
		{
			name: "multi requirement, one req fulfilled by syncset",
			inputs: []string{
				fixtures.TemplateReferentialMultReqs,
				fixtures.SyncSet,
			},
			want: map[string][]int{
				"k8suniqueingresshostmultireq": {2},
			},
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
			want: map[string][]int{
				"k8suniqueingresshostmultireq": {2},
			},
		},
		{
			name:   "no data of any kind",
			inputs: []string{},
			want:   map[string][]int{},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// convert the test resources to unstructureds
			var objs []*unstructured.Unstructured
			for _, input := range tc.inputs {
				u, err := readUnstructured([]byte(input))
				require.NoError(t, err)
				objs = append(objs, u)
			}

			got, err := Verify(objs, tc.discovery)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else if err != nil {
				require.NoError(t, err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("diff in missingRequirements objects (-want +got):\n%s", diff)
			}
		})
	}
}

func readUnstructured(bytes []byte) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{
		Object: make(map[string]interface{}),
	}

	err := yaml.Unmarshal(bytes, u)
	if err != nil {
		return nil, err
	}

	return u, nil
}
