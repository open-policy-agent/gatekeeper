package validate

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/pkg/gator/fixtures"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var (
	constraintNeverValidate    *unstructured.Unstructured
	constraintReferential      *unstructured.Unstructured
	object                     *unstructured.Unstructured
	objectReferentialInventory *unstructured.Unstructured
	objectReferentialDeny      *unstructured.Unstructured
)

func init() {
	var err error
	constraintNeverValidate, err = readUnstructured([]byte(fixtures.ConstraintNeverValidate))
	if err != nil {
		panic(err)
	}

	constraintReferential, err = readUnstructured([]byte(fixtures.ConstraintReferential))
	if err != nil {
		panic(err)
	}

	object, err = readUnstructured([]byte(fixtures.Object))
	if err != nil {
		panic(err)
	}

	objectReferentialInventory, err = readUnstructured([]byte(fixtures.ObjectReferentialInventory))
	if err != nil {
		panic(err)
	}

	objectReferentialDeny, err = readUnstructured([]byte(fixtures.ObjectReferentialDeny))
	if err != nil {
		panic(err)
	}
}

func TestValidate(t *testing.T) {
	tcs := []struct {
		name   string
		inputs []string
		want   []*types.Result
		err    error
	}{
		{
			name: "basic no violation",
			inputs: []string{
				fixtures.TemplateAlwaysValidate,
				fixtures.ConstraintAlwaysValidate,
				fixtures.Object,
			},
		},
		{
			name: "basic violation",
			inputs: []string{
				fixtures.TemplateNeverValidate,
				fixtures.ConstraintNeverValidate,
				fixtures.Object,
			},
			want: []*types.Result{
				{
					Msg:        "never validate",
					Constraint: constraintNeverValidate,
					Resource:   object,
				},
			},
		},
		{
			name: "referential constraint with violation",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.ConstraintReferential,
				fixtures.ObjectReferentialInventory,
				fixtures.ObjectReferentialDeny,
			},
			want: []*types.Result{
				{
					Msg:        "same selector as service <gatekeeper-test-service-disallowed> in namespace <default>",
					Constraint: constraintReferential,
					Resource:   objectReferentialInventory,
				},
				{
					Msg:        "same selector as service <gatekeeper-test-service-example> in namespace <default>",
					Constraint: constraintReferential,
					Resource:   objectReferentialDeny,
				},
			},
		},
		{
			name: "referential constraint without violation",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.ConstraintReferential,
				fixtures.ObjectReferentialInventory,
				fixtures.ObjectReferentialAllow,
			},
			want: nil,
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
			if tc.err != nil {
				// If we're checking for specific errors, use errors.Is() to verify
				if err == nil {
					t.Errorf("got nil err, want %v", tc.err)
				}
			} else {
				if err != nil {
					t.Errorf("got err '%v', want nil", err)
				}
			}

			got := resps.Results()

			diff := cmp.Diff(tc.want, got, cmpopts.IgnoreFields(types.Result{}, "Metadata", "EnforcementAction", "Review"))
			if diff != "" {
				t.Errorf("diff in Result objects (-want +got):\n%s", diff)
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
