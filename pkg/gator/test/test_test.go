package test

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	constraintclienterrors "github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/pkg/gator/fixtures"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var (
	templateNeverValidate      *unstructured.Unstructured
	constraintNeverValidate    *unstructured.Unstructured
	constraintReferential      *unstructured.Unstructured
	object                     *unstructured.Unstructured
	objectReferentialInventory *unstructured.Unstructured
	objectReferentialDeny      *unstructured.Unstructured
)

func init() {
	var err error
	templateNeverValidate, err = readUnstructured([]byte(fixtures.TemplateNeverValidate))
	if err != nil {
		panic(err)
	}

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

func TestTest(t *testing.T) {
	tcs := []struct {
		name           string
		inputs         []string
		want           []*GatorResult
		err            error
		optionMutators []TestOptionMutator
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
			want: []*GatorResult{
				{
					Result: types.Result{
						Target:     target.Name,
						Msg:        "never validate",
						Constraint: constraintNeverValidate,
					},
				},
				{
					Result: types.Result{
						Target:     target.Name,
						Msg:        "never validate",
						Constraint: constraintNeverValidate,
					},
				},
				{
					Result: types.Result{
						Target:     target.Name,
						Msg:        "never validate",
						Constraint: constraintNeverValidate,
					},
				},
			},
		},
		{
			name: "referential constraint with violation, referential data enabled",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.ConstraintReferential,
				fixtures.ObjectReferentialInventory,
				fixtures.ObjectReferentialDeny,
			},
			want: []*GatorResult{
				{
					Result: types.Result{
						Target:     target.Name,
						Msg:        "same selector as service <gatekeeper-test-service-disallowed> in namespace <default>",
						Constraint: constraintReferential,
					},
				},
				{
					Result: types.Result{
						Target:     target.Name,
						Msg:        "same selector as service <gatekeeper-test-service-example> in namespace <default>",
						Constraint: constraintReferential,
					},
				},
			},
		},
		{
			name: "referential constraint with violation, referential data disabled",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.ConstraintReferential,
				fixtures.ObjectReferentialInventory,
				fixtures.ObjectReferentialDeny,
			},
			optionMutators: []TestOptionMutator{DisableReferentialData},
			err:            constraintclienterrors.ErrInvalidConstraintTemplate,
			// QUESTION FOR MAX: This really should be the below error.
			// However, the errors.Is() check isn't working for the commented error.  I traced the
			// error through the constraint framework and back to here and it
			// appears to be using the %w directive at each level, which is
			// what makes the wrapping work (AFAICT).  Perhaps you can see what
			// I'm doing wrong
			// err:            regorewriter.ErrDataReferences,
		},
		{
			name: "referential constraint without violation",
			inputs: []string{
				fixtures.TemplateReferential,
				fixtures.ConstraintReferential,
				fixtures.ObjectReferentialInventory,
				fixtures.ObjectReferentialAllow,
			},
		},
		{
			name:   "no data of any kind",
			inputs: []string{},
		},
		{
			name: "objects with no policy",
			inputs: []string{
				fixtures.ObjectReferentialInventory,
				fixtures.ObjectReferentialAllow,
			},
		},
		{
			name: "template with no objects or constraints",
			inputs: []string{
				fixtures.TemplateReferential,
			},
		},
		{
			name: "constraint with no template causes error",
			inputs: []string{
				fixtures.ConstraintReferential,
			},
			err: constraintclient.ErrMissingConstraintTemplate,
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

			resps, err := Test(objs, tc.optionMutators...)
			if tc.err != nil {
				if err == nil {
					t.Errorf("got nil err, want %v", tc.err)
				}
				if !errors.Is(err, tc.err) {
					t.Errorf("got err %q, want %q", err, tc.err)
				}
			} else if err != nil {
				t.Errorf("got err '%v', want nil", err)
			}

			got := resps.Results()

			diff := cmp.Diff(tc.want, got, cmpopts.IgnoreFields(GatorResult{}, "Metadata", "EnforcementAction", "ViolatingObject"))
			if diff != "" {
				t.Errorf("diff in GatorResult objects (-want +got):\n%s", diff)
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
