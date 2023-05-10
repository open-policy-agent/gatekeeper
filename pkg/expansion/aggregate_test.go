package expansion

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/stretchr/testify/require"
)

// addFn is used to easily build types.Responses within the tests.
type addFn func(base *types.Responses)

func TestAggregateResponses(t *testing.T) {
	tests := []struct {
		name         string
		templateName string
		parent       []addFn
		child        []addFn
		want         []addFn
	}{
		{
			name:         "parent with 1 violation",
			templateName: "foo",
			parent: []addFn{
				addViolation("targetA", "parent violation"),
			},
			child: []addFn{addViolation("targetA", "child violation")},
			want: []addFn{
				addViolation("targetA", "parent violation"),
				addViolation("targetA", prefixMsg("foo", "child violation")),
			},
		},
		{
			name:         "parent with 2 violations",
			templateName: "foo",
			parent: []addFn{
				addViolation("targetA", "parent violation"),
			},
			child: []addFn{
				addViolation("targetA", "child violation A"),
				addViolation("targetA", "child violation B"),
			},
			want: []addFn{
				addViolation("targetA", "parent violation"),
				addViolation("targetA", prefixMsg("foo", "child violation A")),
				addViolation("targetA", prefixMsg("foo", "child violation B")),
			},
		},
		{
			name:         "parent with child with 2 violations, different targets",
			templateName: "foo",
			parent: []addFn{
				addViolation("targetA", "parent violation"),
			},
			child: []addFn{
				addViolation("targetA", "child violation 1"),
				addViolation("targetB", "child violation 2"),
			},
			want: []addFn{
				addViolation("targetA", "parent violation"),
				addViolation("targetA", prefixMsg("foo", "child violation 1")),
				addViolation("targetB", prefixMsg("foo", "child violation 2")),
			},
		},
		{
			name:         "parent with no children",
			templateName: "foo",
			parent: []addFn{
				addViolation("targetA", "parent violation"),
			},
			child: []addFn{},
			want: []addFn{
				addViolation("targetA", "parent violation"),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parent := buildResponses(tc.parent)
			child := buildResponses(tc.child)

			// AggregateResponses will change parent in-place
			AggregateResponses(tc.templateName, parent, child)
			want := buildResponses(tc.want)

			if diff := cmp.Diff(parent, want); diff != "" {
				t.Errorf("got parent: %v\nbut wanted: %v\ndiff: %s", parent, want, diff)
			}
		})
	}
}

// Test_AggregateStats is meant to show that StatsEntry objects are properly added
// fron child to parent when using the AggregateStats function. IT IS NOT meant to
// test StatsEntry objects themselves.
func Test_AggregateStats(t *testing.T) {
	tests := []struct {
		name          string
		templateName  string
		parent        *types.Responses
		child         *types.Responses
		expectedStats int
	}{
		{
			name:          "empty parent and child",
			templateName:  "testTemplate1",
			parent:        &types.Responses{},
			child:         &types.Responses{},
			expectedStats: 0,
		},
		{
			name:         "empty parent, single child",
			templateName: "testTemplate2",
			parent:       &types.Responses{},
			child: &types.Responses{
				StatsEntries: []*instrumentation.StatsEntry{
					{Labels: []*instrumentation.Label{}},
				},
			},
			expectedStats: 1,
		},
		{
			name:         "multiple children",
			templateName: "testTemplate3",
			parent: &types.Responses{
				StatsEntries: []*instrumentation.StatsEntry{
					{Labels: []*instrumentation.Label{}},
				},
			},
			child: &types.Responses{
				StatsEntries: []*instrumentation.StatsEntry{
					{Labels: []*instrumentation.Label{}},
					{Labels: []*instrumentation.Label{}},
				},
			},
			expectedStats: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parent := tc.parent
			child := tc.child
			AggregateStats(tc.templateName, parent, child)

			require.Len(t, parent.StatsEntries, tc.expectedStats)

			for _, se := range tc.parent.StatsEntries {
				labelFound := false
				for _, label := range se.Labels {
					if label.Name == ChildStatLabel && label.Value == tc.templateName {
						labelFound = true
						break
					}
				}

				// note that, in practice, we shouldn't care for the len of se.Labels
				// we only add the check in here to distinguish between the parent labels,
				// which are 0, and the child labels which should have at least one element,
				// the label that is added thru AggregateStats
				if !labelFound && len(se.Labels) > 0 {
					t.Errorf("expected label %s not found", ChildStatLabel)
				}
			}
		})
	}
}

func TestOverrideEnforcementAction(t *testing.T) {
	tests := []struct {
		name  string
		ea    string
		resps *types.Responses
		want  *types.Responses
	}{
		{
			name: "empty enforcement action override does not change response",
			ea:   "",
			resps: &types.Responses{
				ByTarget: map[string]*types.Response{
					"targetA": {
						Target: "targetA",
						Results: []*types.Result{
							{
								Target:            "targetA",
								Msg:               "violationA",
								EnforcementAction: "deny",
							},
						},
					},
				},
			},
			want: &types.Responses{
				ByTarget: map[string]*types.Response{
					"targetA": {
						Target: "targetA",
						Results: []*types.Result{
							{
								Target:            "targetA",
								Msg:               "violationA",
								EnforcementAction: "deny",
							},
						},
					},
				},
			},
		},
		{
			name: "deny enforcement action override with multiple targets and results",
			ea:   "deny",
			resps: &types.Responses{
				ByTarget: map[string]*types.Response{
					"targetA": {
						Target: "targetA",
						Results: []*types.Result{
							{
								Target:            "targetA",
								Msg:               "violationA",
								EnforcementAction: "warn",
							},
						},
					},
					"targetB": {
						Target: "targetB",
						Results: []*types.Result{
							{
								Target:            "targetB",
								Msg:               "violationB",
								EnforcementAction: "warn",
							},
							{
								Target:            "targetB",
								Msg:               "violationC",
								EnforcementAction: "warn",
							},
						},
					},
				},
			},
			want: &types.Responses{
				ByTarget: map[string]*types.Response{
					"targetA": {
						Target: "targetA",
						Results: []*types.Result{
							{
								Target:            "targetA",
								Msg:               "violationA",
								EnforcementAction: "deny",
							},
						},
					},
					"targetB": {
						Target: "targetB",
						Results: []*types.Result{
							{
								Target:            "targetB",
								Msg:               "violationB",
								EnforcementAction: "deny",
							},
							{
								Target:            "targetB",
								Msg:               "violationC",
								EnforcementAction: "deny",
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			OverrideEnforcementAction(tc.ea, tc.resps)
			if diff := cmp.Diff(tc.resps, tc.want); diff != "" {
				t.Errorf("got : %v\nbut wanted: %v\ndiff: %s", tc.resps, tc.want, diff)
			}
		})
	}
}

func buildResponses(fns []addFn) *types.Responses {
	r := &types.Responses{ByTarget: make(map[string]*types.Response)}
	for _, f := range fns {
		f(r)
	}
	return r
}

func prefixMsg(parentName string, msg string) string {
	return fmt.Sprintf("%s %s", fmt.Sprintf(childMsgPrefix, parentName), msg)
}

func addViolation(target string, msgs ...string) func(base *types.Responses) {
	return func(base *types.Responses) {
		if _, exists := base.ByTarget[target]; !exists {
			base.ByTarget[target] = &types.Response{Target: target}
		}

		results := make([]*types.Result, len(msgs))
		for i, m := range msgs {
			results[i] = &types.Result{Target: target, Msg: m}
		}

		base.ByTarget[target].Results = append(base.ByTarget[target].Results, results...)
	}
}
