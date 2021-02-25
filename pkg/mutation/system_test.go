package mutation

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// Leveraging existing resource types to create custom mutators in order to validate
// the cache.
type MockMutator struct {
	Mocked           types.ID
	RelevantField    string // relevant for comparison
	NotRelevantField string // not relevant for comparison
	Labels           map[string]string
	MutationCount    int
	UnstableFor      int // makes the mutation unstable for the first n mutations
}

func (m *MockMutator) Matches(obj runtime.Object, ns *corev1.Namespace) bool {
	return true // always matches
}

func (m *MockMutator) Mutate(obj *unstructured.Unstructured) (bool, error) {
	m.MutationCount++
	if m.Labels == nil {
		return false, nil
	}
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return false, err
	}

	current := accessor.GetLabels()
	if current == nil {
		current = make(map[string]string)
	}

	for k, v := range m.Labels {
		if m.MutationCount < m.UnstableFor { // means we need to make the mutation unstable, adding the count
			v = fmt.Sprintf("%s%d", v, m.MutationCount)
		}
		current[k] = v
	}
	accessor.SetLabels(current)

	return true, nil
}

func (m *MockMutator) ID() types.ID {
	return m.Mocked
}

func (m *MockMutator) Path() *parser.Path {
	return nil
}

func (m *MockMutator) Value() (interface{}, error) {
	return nil, nil
}

func (m *MockMutator) HasDiff(mutator types.Mutator) bool {
	mock, ok := mutator.(*MockMutator)
	if !ok {
		return false
	}
	return m.RelevantField != mock.RelevantField
}

func (m *MockMutator) DeepCopy() types.Mutator {
	res := &MockMutator{
		Mocked:           m.Mocked,
		RelevantField:    m.RelevantField,
		NotRelevantField: m.NotRelevantField,
		MutationCount:    m.MutationCount,
		UnstableFor:      m.UnstableFor,
	}
	if m.Labels != nil {
		if res.Labels == nil {
			res.Labels = make(map[string]string)
		}
		for k, v := range m.Labels {
			res.Labels[k] = v
		}
	}
	return res
}

func (m *MockMutator) String() string {
	return ""
}

var mutators = []types.Mutator{
	&MockMutator{Mocked: types.ID{Group: "bbb", Kind: "aaa", Namespace: "aaa", Name: "aaa"}},
	&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "ccc", Name: "ddd"}},
	&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "aaa", Name: "aaa"}},
	&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "ccc", Name: "ddd"}},
	&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "aaa"}},
	&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "ccc", Name: "aaa"}},
}

func TestSorting(t *testing.T) {
	table := []struct {
		tname    string
		initial  []types.Mutator
		expected []types.Mutator
		action   func(*System) error
	}{
		{
			tname:   "testsort",
			initial: mutators,
			expected: []types.Mutator{
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "aaa"}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "ccc", Name: "ddd"}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "aaa", Name: "aaa"}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "ccc", Name: "aaa"}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "ccc", Name: "ddd"}},
				&MockMutator{Mocked: types.ID{Group: "bbb", Kind: "aaa", Namespace: "aaa", Name: "aaa"}},
			},
			action: func(s *System) error { return nil },
		},
		{
			tname:   "testremove",
			initial: mutators,
			expected: []types.Mutator{
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "aaa"}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "ccc", Name: "ddd"}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "aaa", Name: "aaa"}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "ccc", Name: "ddd"}},
				&MockMutator{Mocked: types.ID{Group: "bbb", Kind: "aaa", Namespace: "aaa", Name: "aaa"}},
			},
			action: func(s *System) error {
				return s.Remove(types.ID{Group: "aaa", Kind: "bbb", Namespace: "ccc", Name: "aaa"})
			},
		},
		{
			tname:   "testaddingsame",
			initial: mutators,
			expected: []types.Mutator{
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "aaa"}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "ccc", Name: "ddd"}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "aaa", Name: "aaa"}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "ccc", Name: "aaa"}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "ccc", Name: "ddd"}},
				&MockMutator{Mocked: types.ID{Group: "bbb", Kind: "aaa", Namespace: "aaa", Name: "aaa"}},
			},
			action: func(s *System) error {
				return s.Upsert(&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "ccc", Name: "aaa"},
					NotRelevantField: "notrelevantvalue"})
			},
		},
		{
			tname:   "testaddingdifferent",
			initial: mutators,
			expected: []types.Mutator{
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "aaa"}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "ccc", Name: "ddd"}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "aaa", Name: "aaa"}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "ccc", Name: "aaa"}, RelevantField: "relevantvalue"},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "ccc", Name: "ddd"}},
				&MockMutator{Mocked: types.ID{Group: "bbb", Kind: "aaa", Namespace: "aaa", Name: "aaa"}},
			},
			action: func(s *System) error {
				return s.Upsert(&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "bbb", Namespace: "ccc", Name: "aaa"},
					RelevantField: "relevantvalue"})
			},
		},
	}

	for _, tc := range table {
		t.Run(tc.tname, func(t *testing.T) {
			c := NewSystem()
			for i, m := range tc.initial {
				err := c.Upsert(m)
				if err != nil {
					t.Errorf("%s: Failed inserting %dth object", tc.tname, i)
				}
			}
			err := tc.action(c)
			if err != nil {
				t.Errorf("%s: test action failed %v", tc.tname, err)
			}
			if len(c.orderedMutators) != len(tc.expected) {
				t.Errorf("%s: Expected %d object from the operator, found %d", tc.tname, len(c.orderedMutators), len(tc.expected))
			}

			if !cmp.Equal(c.orderedMutators, tc.expected) {
				t.Errorf("%s: Cache content is not consistent: %s", tc.tname, cmp.Diff(c.orderedMutators, tc.expected))
			}

			expectedMap := make(map[types.ID]types.Mutator)
			for _, m := range tc.expected {
				expectedMap[m.ID()] = m
			}
			if !cmp.Equal(c.mutatorsMap, expectedMap) {
				t.Errorf("%s: Cache content (map) is not consistent: %s", tc.tname, cmp.Diff(c.mutatorsMap, expectedMap))
			}
		})
	}
}

func TestMutation(t *testing.T) {
	table := []struct {
		tname              string
		mutations          [](*MockMutator)
		expectedLabels     map[string]string
		expectedIterations int
		expectError        bool
	}{
		{
			tname: "mutate",
			mutations: [](*MockMutator){
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "aaa"}, Labels: map[string]string{
					"ka": "va",
				}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "bbb"}, Labels: map[string]string{
					"kb": "vb",
				}},
			},
			expectedLabels: map[string]string{
				"ka": "va",
				"kb": "vb",
			},
			expectedIterations: 2,
		},
		{
			tname: "neverconverge",
			mutations: [](*MockMutator){
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "aaa"}, Labels: map[string]string{
					"ka": "va",
				}, UnstableFor: 5},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "bbb"}, Labels: map[string]string{
					"kb": "vb",
				}},
			},
			expectError: true,
		},
		{
			tname: "convergeafter3",
			mutations: [](*MockMutator){
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "aaa"}, Labels: map[string]string{
					"ka": "va",
				}, UnstableFor: 3},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "bbb"}, Labels: map[string]string{
					"kb": "vb",
				}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "ccc"}, Labels: map[string]string{
					"kb": "vb",
				}},
				&MockMutator{Mocked: types.ID{Group: "aaa", Kind: "aaa", Namespace: "aaa", Name: "ddd"}, Labels: map[string]string{
					"kb": "vb",
				}},
			},
			expectedLabels: map[string]string{
				"ka": "va",
				"kb": "vb",
			},
			expectedIterations: 4,
		},
	}
	for _, tc := range table {
		t.Run(tc.tname, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testpod",
					Namespace: "foo",
				},
			}

			converted, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
			if err != nil {
				t.Fatal(tc.tname, "Convert pod to unstructured failed")
			}
			toMutate := &unstructured.Unstructured{Object: converted}

			c := NewSystem()
			for i, m := range tc.mutations {
				err := c.Upsert(m)
				if err != nil {
					t.Errorf(tc.tname, "Failed inserting %dth object", i)
				}
			}
			mutated, err := c.Mutate(toMutate, nil)
			if tc.expectError && err == nil {
				t.Fatal(tc.tname, "Expecting error from mutate, did not fail")
			}

			if tc.expectError { // if error is expected, don't do additional checks
				return
			}

			if err != nil {
				t.Fatal(tc.tname, "Mutate failed unexpectedly", err)
			}

			accessor, err := meta.Accessor(toMutate)
			if err != nil {
				t.Fatal("Failed to get unstruct accessor", err)
			}

			newLabels := accessor.GetLabels()

			if !mutated {
				t.Error(tc.tname, "Mutation not as expected", cmp.Diff(newLabels, tc.expectedLabels))
			}

			if !cmp.Equal(newLabels, tc.expectedLabels) {
				t.Error(tc.tname, "Mutation not as expected", cmp.Diff(newLabels, tc.expectedLabels))
			}

			probe := c.orderedMutators[0].(*MockMutator) // fetching a mock mutator to check the number of iterations
			if probe.MutationCount != tc.expectedIterations {
				t.Error(tc.tname, "Expected %d  mutation iterations, got", tc.expectedIterations, tc.mutations[0].MutationCount)
			}
		})
	}
}
