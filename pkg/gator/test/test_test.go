package test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/fixtures"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var (
	templateNeverValidate      *unstructured.Unstructured
	constraintNeverValidate    *unstructured.Unstructured
	constraintGatorValidate    *unstructured.Unstructured
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

	constraintGatorValidate, err = readUnstructured([]byte(fixtures.ConstraintGatorValidate))
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

func ignoreGatorResultFields() cmp.Option {
	return cmp.FilterPath(
		func(p cmp.Path) bool {
			switch p.String() {
			// ignore these fields
			case "Result.Metadata", "Result.EvaluationMeta", "Result.EnforcementAction", "ViolatingObject":
				return true
			default:
				return false
			}
		},
		cmp.Ignore())
}

func TestTest(t *testing.T) {
	tcs := []struct {
		name      string
		inputs    []string
		want      []*GatorResult
		cmpOption cmp.Option
		err       error
	}{
		{
			name: "basic no violation",
			inputs: []string{
				fixtures.TemplateAlwaysValidate,
				fixtures.ConstraintAlwaysValidate,
				fixtures.Object,
			},
			cmpOption: ignoreGatorResultFields(),
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
			cmpOption: ignoreGatorResultFields(),
		},
		{
			name: "referential constraint with violation",
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
			cmpOption: ignoreGatorResultFields(),
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
		{
			name: "constraint with gator EP",
			inputs: []string{
				fixtures.TemplateNeverValidate,
				fixtures.ConstraintGatorValidate,
				fixtures.Object,
			},
			want: []*GatorResult{
				{
					Result: types.Result{
						Target:                   target.Name,
						Msg:                      "never validate",
						Constraint:               constraintGatorValidate,
						EnforcementAction:        "scoped",
						ScopedEnforcementActions: []string{"deny"},
					},
				},
				{
					Result: types.Result{
						Target:                   target.Name,
						Msg:                      "never validate",
						Constraint:               constraintGatorValidate,
						EnforcementAction:        "scoped",
						ScopedEnforcementActions: []string{"deny"},
					},
				},
				{
					Result: types.Result{
						Target:                   target.Name,
						Msg:                      "never validate",
						Constraint:               constraintGatorValidate,
						EnforcementAction:        "scoped",
						ScopedEnforcementActions: []string{"deny"},
					},
				},
			},
			cmpOption: ignoreGatorResultFields(),
		},
		{
			name: "constraint with audit EP",
			inputs: []string{
				fixtures.TemplateNeverValidate,
				fixtures.ConstraintAuditValidate,
				fixtures.Object,
			},
			cmpOption: ignoreGatorResultFields(),
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

			resps, err := Test(objs, Opts{})
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else if err != nil {
				require.NoError(t, err)
			}

			got := resps.Results()

			var diff string
			if tc.cmpOption != nil {
				diff = cmp.Diff(tc.want, got, tc.cmpOption)
			} else {
				diff = cmp.Diff(tc.want, got)
			}

			if diff != "" {
				t.Errorf("diff in GatorResult objects (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_Test_withTrace proves that we can get a Trace populated when we ask for it.
func Test_Test_withTrace(t *testing.T) {
	inputs := []string{
		fixtures.TemplateNeverValidate,
		fixtures.ConstraintNeverValidate,
		fixtures.Object,
	}

	var objs []*unstructured.Unstructured
	for _, input := range inputs {
		u, err := readUnstructured([]byte(input))
		if err != nil {
			t.Fatalf("readUnstructured for input %q: %v", input, err)
		}
		objs = append(objs, u)
	}

	resps, err := Test(objs, Opts{IncludeTrace: true})
	if err != nil {
		t.Errorf("got err '%v', want nil", err)
	}

	got := resps.Results()

	want := []*GatorResult{
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
	}

	diff := cmp.Diff(want, got, ignoreGatorResultFields(), cmpopts.IgnoreFields(
		GatorResult{},
		"Trace", // ignore Trace for now, we will assert non nil further down
	))
	if diff != "" {
		t.Errorf("diff in GatorResult objects (-want +got):\n%s", diff)
	}

	for _, gotResult := range got {
		if gotResult.Trace == nil {
			t.Errorf("did not find a trace when we expected to find one")
		}
	}
}

// Test_Test_withStats proves that we can get a stats populated when we ask for it.
func Test_Test_withStats(t *testing.T) {
	inputs := []string{
		fixtures.TemplateNeverValidate,
		fixtures.ConstraintNeverValidate,
		fixtures.Object,
	}

	var objs []*unstructured.Unstructured
	for _, input := range inputs {
		u, err := readUnstructured([]byte(input))
		assert.NoErrorf(t, err, "readUnstructured for input %q: %v", input, err)
		objs = append(objs, u)
	}

	resps, err := Test(objs, Opts{GatherStats: true})
	assert.NoError(t, err)

	actualStats := resps.StatsEntries
	expectesSE := &instrumentation.StatsEntry{
		Scope:    "template",
		StatsFor: "NeverValidate",
		Stats: []*instrumentation.Stat{
			{
				Name: "templateRunTimeNS",
				// Value: 0, // will be checked later
				Source: instrumentation.Source{
					Type:  "engine",
					Value: "Rego",
				},
			},
			{
				Name:  "constraintCount",
				Value: 1,
				Source: instrumentation.Source{
					Type:  "engine",
					Value: "Rego",
				},
			},
		},
		Labels: []*instrumentation.Label{
			{
				Name:  "TracingEnabled",
				Value: false,
			},
			{
				Name:  "PrintEnabled",
				Value: false,
			},
			{
				Name:  "target",
				Value: "admission.k8s.gatekeeper.sh",
			},
		},
	}

	// test.go calls review on all 3 objects (template, constraint, objToReview)
	// so we need 3 "almost" copies of the stat entry above.
	expectedStats := []*instrumentation.StatsEntry{}
	expectedStats = append(expectedStats, expectesSE, expectesSE, expectesSE)

	diff := cmp.Diff(actualStats, expectedStats, cmpopts.IgnoreFields(
		instrumentation.Stat{}, "Value",
	))
	if diff != "" {
		t.Errorf("diff in StatsEntries (-want +got):\n%s", diff)
	}

	// there should be at least 3 stats entry with two stats each
	assert.Len(t, actualStats, len(expectedStats))
	assert.Len(t, actualStats[0].Stats, 2)
	for _, stat := range actualStats[0].Stats {
		if stat.Name == "templateRunTimeNS" {
			require.NotZero(t, stat.Value)
		}
		if stat.Name == "constraintCount" {
			require.Equal(t, stat.Value, 1)
		}
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
