package expansion

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
)

// addFn is used to easily build types.Responses within the tests
type addFn func(base *types.Responses)

func TestAggregateResponses(t *testing.T) {
	tests := []struct {
		name       string
		parentName string
		parent     []addFn
		children   [][]addFn
		want       []addFn
	}{
		{
			name:       "parent with 1 child with 1 violation",
			parentName: "foo",
			parent: []addFn{
				addViolation("targetA", "parent violation"),
			},
			children: [][]addFn{
				{
					addViolation("targetA", "child violation"),
				},
			},
			want: []addFn{
				addViolation("targetA", "parent violation"),
				addViolation("targetA", prefixMsg("foo", "child violation")),
			},
		},
		{
			name:       "parent with 2 children each with 2 violations",
			parentName: "foo",
			parent: []addFn{
				addViolation("targetA", "parent violation"),
			},
			children: [][]addFn{
				{
					addViolation("targetA", "child violation A"),
					addViolation("targetA", "child violation B"),
				},
				{
					addViolation("targetA", "child violation C"),
					addViolation("targetA", "child violation D"),
				},
			},
			want: []addFn{
				addViolation("targetA", "parent violation"),
				addViolation("targetA", prefixMsg("foo", "child violation A")),
				addViolation("targetA", prefixMsg("foo", "child violation B")),
				addViolation("targetA", prefixMsg("foo", "child violation C")),
				addViolation("targetA", prefixMsg("foo", "child violation D")),
			},
		},
		{
			name:       "parent with 2 children each with 2 violations different targets",
			parentName: "foo",
			parent: []addFn{
				addViolation("targetA", "parent violation"),
			},
			children: [][]addFn{
				{
					addViolation("targetA", "child violation 1"),
					addViolation("targetB", "child violation 2"),
				},
				{
					addViolation("targetA", "child violation 3"),
					addViolation("targetB", "child violation 4"),
				},
			},
			want: []addFn{
				addViolation("targetA", "parent violation"),
				addViolation("targetA", prefixMsg("foo", "child violation 1")),
				addViolation("targetB", prefixMsg("foo", "child violation 2")),
				addViolation("targetA", prefixMsg("foo", "child violation 3")),
				addViolation("targetB", prefixMsg("foo", "child violation 4")),
			},
		},
		{
			name:       "parent with no children",
			parentName: "foo",
			parent: []addFn{
				addViolation("targetA", "parent violation"),
			},
			children: [][]addFn{},
			want: []addFn{
				addViolation("targetA", "parent violation"),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parent := buildResponses(tc.parent)
			children := make([]*types.Responses, len(tc.children))
			for i, _ := range tc.children {
				children[i] = buildResponses(tc.children[i])
			}

			// AggregateResponses will change parent in-place
			AggregateResponses(tc.parentName, parent, children)
			want := buildResponses(tc.want)

			if diff := cmp.Diff(parent, want); diff != "" {
				t.Errorf("got parent: %v\nbut wanted: %v\ndiff: %s", parent, want, diff)
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
