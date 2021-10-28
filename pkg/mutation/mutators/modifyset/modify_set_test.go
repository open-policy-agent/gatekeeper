package modifyset

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
)

func TestSetterMerge(t *testing.T) {
	tcs := []struct {
		name        string
		vals        []interface{}
		existing    interface{}
		expected    []interface{}
		errExpected bool
	}{
		{
			name:        "Error on non-list",
			vals:        []interface{}{"a"},
			existing:    7,
			errExpected: true,
		},
		{
			name:     "Empty vals",
			vals:     []interface{}{},
			existing: []interface{}{"a", "b", "c"},
			expected: []interface{}{"a", "b", "c"},
		},
		{
			name:     "Nil vals",
			existing: []interface{}{"a", "b", "c"},
			expected: []interface{}{"a", "b", "c"},
		},
		{
			name:     "Duplicate vals",
			vals:     []interface{}{"a", "b"},
			existing: []interface{}{"a", "b", "c"},
			expected: []interface{}{"a", "b", "c"},
		},
		{
			name:     "Overlapping vals",
			vals:     []interface{}{"a", "b", "d"},
			existing: []interface{}{"a", "b", "c"},
			expected: []interface{}{"a", "b", "c", "d"},
		},
		{
			name:     "Nil existing",
			vals:     []interface{}{"a", "b", "d"},
			expected: []interface{}{"a", "b", "d"},
		},
		{
			name:     "Empty list existing",
			vals:     []interface{}{"a", "b", "d"},
			existing: []interface{}{},
			expected: []interface{}{"a", "b", "d"},
		},
		{
			name:     "Non-standard members",
			vals:     []interface{}{[]interface{}{"a", "b", "d"}, []interface{}{"z", "y", "q"}},
			existing: []interface{}{[]interface{}{"a", "b", "d"}, []interface{}{"r", "r", "r"}},
			expected: []interface{}{[]interface{}{"a", "b", "d"}, []interface{}{"r", "r", "r"}, []interface{}{"z", "y", "q"}},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			s := setter{
				values: tc.vals,
				op:     mutationsunversioned.MergeOp,
			}
			obj := map[string]interface{}{}
			if tc.existing != nil {
				obj["field"] = tc.existing
			}
			err := s.SetValue(obj, "field")
			if err != nil != tc.errExpected {
				t.Errorf("err = %+v, wanted %v", err, tc.errExpected)
			}
			if tc.expected != nil {
				if !cmp.Equal(tc.expected, obj["field"]) {
					t.Errorf("got %+v, wanted %+v", obj["field"], tc.expected)
				}
			}
		})
	}
}

func TestSetterPrune(t *testing.T) {
	tcs := []struct {
		name        string
		vals        []interface{}
		existing    interface{}
		expected    []interface{}
		errExpected bool
	}{
		{
			name:        "Error on non-list",
			vals:        []interface{}{"a"},
			existing:    7,
			errExpected: true,
		},
		{
			name:     "Empty vals",
			vals:     []interface{}{},
			existing: []interface{}{"a", "b", "c"},
			expected: []interface{}{"a", "b", "c"},
		},
		{
			name:     "Nil vals",
			existing: []interface{}{"a", "b", "c"},
			expected: []interface{}{"a", "b", "c"},
		},
		{
			name:     "Duplicate vals",
			vals:     []interface{}{"a", "b"},
			existing: []interface{}{"a", "b", "c"},
			expected: []interface{}{"c"},
		},
		{
			name:     "Overlapping vals",
			vals:     []interface{}{"a", "b", "d"},
			existing: []interface{}{"a", "b", "c"},
			expected: []interface{}{"c"},
		},
		{
			name:     "Nil existing",
			vals:     []interface{}{"a", "b", "d"},
			expected: nil,
		},
		{
			name:     "Empty list existing",
			vals:     []interface{}{"a", "b", "d"},
			existing: []interface{}{},
			expected: []interface{}{},
		},
		{
			name:     "Non-standard members",
			vals:     []interface{}{[]interface{}{"a", "b", "d"}, []interface{}{"z", "y", "q"}},
			existing: []interface{}{[]interface{}{"a", "b", "d"}, []interface{}{"r", "r", "r"}},
			expected: []interface{}{[]interface{}{"r", "r", "r"}},
		},
		{
			name:     "Duplicate existing",
			vals:     []interface{}{"a"},
			existing: []interface{}{"a", "b", "a"},
			expected: []interface{}{"b"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			s := setter{
				values: tc.vals,
				op:     mutationsunversioned.PruneOp,
			}
			obj := map[string]interface{}{}
			if tc.existing != nil {
				obj["field"] = tc.existing
			}
			err := s.SetValue(obj, "field")
			if err != nil != tc.errExpected {
				t.Errorf("err = %+v, wanted %v", err, tc.errExpected)
			}
			if tc.expected != nil {
				if !cmp.Equal(tc.expected, obj["field"]) {
					t.Errorf("got %+v, wanted %+v", obj["field"], tc.expected)
				}
			}
		})
	}
}
