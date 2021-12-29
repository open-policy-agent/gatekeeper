package validate

import (
	"testing"

	"github.com/open-policy-agent/gatekeeper/pkg/gator/fixtures"
	y3 "gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestValidate(t *testing.T) {
	tcs := []struct {
		name   string
		inputs []string
		want   int
		err    bool
	}{
		{
			name: "basic no violation",
			inputs: []string{
				fixtures.TemplateAlwaysValidate,
				fixtures.ConstraintAlwaysValidate,
				fixtures.Object,
				fixtures.ObjectIncluded,
			},
		},
		{
			name: "basic violation",
			inputs: []string{
				fixtures.TemplateNeverValidate,
				fixtures.ConstraintNeverValidate,
				fixtures.Object,
				fixtures.ObjectIncluded,
			},
			want: 2,
		},
		{
			name: "referential constraint with violation",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.ConstraintReferential,
				fixtures.ObjectReferentialInventory,
				fixtures.ObjectReferentialDeny,
			},
			want: 2,
		},
		{
			name: "referential constraint without violation",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.ConstraintReferential,
				fixtures.ObjectReferentialInventory,
				fixtures.ObjectReferentialAllow,
			},
			want: 0,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// convert the test resources to unstructureds
			var objs []*unstructured.Unstructured
			for _, input := range tc.inputs {
				u, err := readUnstructured([]byte(input))
				if err != nil {
					t.Fatalf("readUnstructured for input %q: %v", input, err)
				}
				objs = append(objs, u)
			}

			resps, err := Validate(objs)
			if tc.err {
				if err == nil {
					t.Errorf("got nil err, want err")
				}
			} else {
				if err != nil {
					t.Errorf("got err '%v', want nil", err)
				}
			}

			got := len(resps.Results())
			if got != tc.want {
				t.Errorf("got %v results, want: %v", got, tc.want)
				for _, result := range resps.Results() {
					y, _ := y3.Marshal(result)
					t.Errorf("result: \n%s", string(y))
				}
				t.FailNow()
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
