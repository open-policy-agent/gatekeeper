package expansion

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
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
