package mutators

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/tester"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/wildcard"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func makeValue(v interface{}) mutationsunversioned.AssignField {
	return mutationsunversioned.AssignField{Value: &types.Anything{Value: v}}
}

func TestAssignToMutator(t *testing.T) {
	assign := &mutationsunversioned.Assign{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: mutationsunversioned.AssignSpec{
			ApplyTo: []match.MutationApplyTo{
				{ApplyTo: match.ApplyTo{
					Groups:   []string{"group1", "group2"},
					Kinds:    []string{"kind1", "kind2", "kind3"},
					Versions: []string{"version1"},
				}},
				{ApplyTo: match.ApplyTo{
					Groups:   []string{"group3", "group4"},
					Kinds:    []string{"kind4", "kind2", "kind3"},
					Versions: []string{"version1"},
				}},
			},
			Match:    match.Match{},
			Location: "spec.foo",
			Parameters: mutationsunversioned.Parameters{
				Assign: makeValue("foobar"),
			},
		},
	}

	mutatorWithSchema, err := MutatorForAssign(assign)
	if err != nil {
		t.Fatalf("MutatorForAssign failed, %v", err)
	}

	bindings := mutatorWithSchema.SchemaBindings()
	expectedBindings := []schema.GroupVersionKind{
		{Group: "group1", Version: "version1", Kind: "kind1"},
		{Group: "group1", Version: "version1", Kind: "kind2"},
		{Group: "group1", Version: "version1", Kind: "kind3"},
		{Group: "group2", Version: "version1", Kind: "kind1"},
		{Group: "group2", Version: "version1", Kind: "kind2"},
		{Group: "group2", Version: "version1", Kind: "kind3"},
		{Group: "group3", Version: "version1", Kind: "kind2"},
		{Group: "group3", Version: "version1", Kind: "kind3"},
		{Group: "group3", Version: "version1", Kind: "kind4"},
		{Group: "group4", Version: "version1", Kind: "kind2"},
		{Group: "group4", Version: "version1", Kind: "kind3"},
		{Group: "group4", Version: "version1", Kind: "kind4"},
	}

	if diff := cmp.Diff(expectedBindings, bindings); diff != "" {
		t.Errorf("Bindings are not as expected: %s", diff)
	}
}

func TestAssignMetadataToMutator(t *testing.T) {
	assignMeta := &mutationsunversioned.AssignMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: mutationsunversioned.AssignMetadataSpec{
			Match:    match.Match{},
			Location: "metadata.labels.foo",
			Parameters: mutationsunversioned.MetadataParameters{
				Assign: makeValue("foobar"),
			},
		},
	}

	mutator, err := MutatorForAssignMetadata(assignMeta)
	if err != nil {
		t.Fatalf("MutatorForAssignMetadata for failed, %v", err)
	}
	path := mutator.Path()
	if len(path.Nodes) == 0 {
		t.Fatalf("Got empty path")
	}
}

func TestAssignHasDiff(t *testing.T) {
	first := &mutationsunversioned.Assign{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: mutationsunversioned.AssignSpec{
			ApplyTo: []match.MutationApplyTo{
				{ApplyTo: match.ApplyTo{
					Groups:   []string{"group1", "group2"},
					Kinds:    []string{"kind1", "kind2", "kind3"},
					Versions: []string{"version1"},
				}},
				{ApplyTo: match.ApplyTo{
					Groups:   []string{"group3", "group4"},
					Kinds:    []string{"kind4", "kind2", "kind3"},
					Versions: []string{"version1"},
				}},
			},
			Match:    match.Match{},
			Location: "spec.foo",
			Parameters: mutationsunversioned.Parameters{
				Assign: makeValue("foobar"),
			},
		},
	}
	// This is normally filled during the serialization
	gvk := schema.GroupVersionKind{
		Kind:    "kindname",
		Group:   "groupname",
		Version: "versionname",
	}
	first.APIVersion, first.Kind = gvk.ToAPIVersionAndKind()

	second := first.DeepCopy()

	table := []struct {
		tname        string
		modify       func(*mutationsunversioned.Assign)
		areDifferent bool
	}{
		{
			"same",
			func(_ *mutationsunversioned.Assign) {},
			false,
		},
		{
			"differentApplyTo",
			func(a *mutationsunversioned.Assign) {
				a.Spec.ApplyTo[1].Kinds[0] = "kind"
			},
			true,
		},
		{
			"differentLocation",
			func(a *mutationsunversioned.Assign) {
				a.Spec.Location = "bar"
			},
			true,
		},
		{
			"differentParameters",
			func(a *mutationsunversioned.Assign) {
				a.Spec.Parameters.PathTests = []mutationsunversioned.PathTest{{SubPath: "spec", Condition: tester.MustExist}}
			},
			true,
		},
	}

	for _, tc := range table {
		t.Run(tc.tname, func(t *testing.T) {
			secondAssign := second.DeepCopy()
			tc.modify(secondAssign)
			firstMutator, err := MutatorForAssign(first)
			if err != nil {
				t.Fatal(tc.tname, "Failed to convert first to mutator", err)
			}
			secondMutator, err := MutatorForAssign(secondAssign)
			if err != nil {
				t.Fatal(tc.tname, "Failed to convert second to mutator", err)
			}
			hasDiff := firstMutator.HasDiff(secondMutator)
			hasDiff1 := secondMutator.HasDiff(firstMutator)
			if hasDiff != hasDiff1 {
				t.Error(tc.tname, "Diff first from second is different from second to first")
			}
			if hasDiff != tc.areDifferent {
				t.Errorf("test %s: Expected to be different: %v, diff result is %v", tc.tname, tc.areDifferent, hasDiff)
			}
		})
	}
}

func TestAssignMetadataHasDiff(t *testing.T) {
	first := &mutationsunversioned.AssignMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: mutationsunversioned.AssignMetadataSpec{
			Match:    match.Match{},
			Location: "metadata.labels.foo",
			Parameters: mutationsunversioned.MetadataParameters{
				Assign: makeValue("foobar"),
			},
		},
	}

	// This is normally filled during the serialization
	gvk := schema.GroupVersionKind{
		Kind:    "kindname",
		Group:   "groupname",
		Version: "versionname",
	}
	first.APIVersion, first.Kind = gvk.ToAPIVersionAndKind()

	second := first.DeepCopy()

	table := []struct {
		tname        string
		modify       func(*mutationsunversioned.AssignMetadata)
		areDifferent bool
	}{
		{
			"same",
			func(_ *mutationsunversioned.AssignMetadata) {},
			false,
		},
		{
			"differentLocation",
			func(a *mutationsunversioned.AssignMetadata) {
				a.Spec.Location = "metadata.annotations.bar"
			},
			true,
		},
		{
			"differentName",
			func(a *mutationsunversioned.AssignMetadata) {
				a.Name = "anothername"
			},
			true,
		},
		{
			"differentMatch",
			func(a *mutationsunversioned.AssignMetadata) {
				a.Spec.Match.Namespaces = []wildcard.Wildcard{"foo", "bar"}
			},
			true,
		},
	}

	for _, tc := range table {
		t.Run(tc.tname, func(t *testing.T) {
			secondAssignMeta := second.DeepCopy()
			tc.modify(secondAssignMeta)
			firstMutator, err := MutatorForAssignMetadata(first)
			if err != nil {
				t.Fatal(tc.tname, "Failed to convert first to mutator", err)
			}
			secondMutator, err := MutatorForAssignMetadata(secondAssignMeta)
			if err != nil {
				t.Fatal(tc.tname, "Failed to convert second to mutator", err)
			}
			hasDiff := firstMutator.HasDiff(secondMutator)
			hasDiff1 := secondMutator.HasDiff(firstMutator)
			if hasDiff != hasDiff1 {
				t.Error(tc.tname, "Diff first from second is different from second to first")
			}
			if hasDiff != tc.areDifferent {
				t.Errorf("test %s: Expected to be different: %v, diff result is %v", tc.tname, tc.areDifferent, hasDiff)
			}
		})
	}
}

func TestParseShouldFail(t *testing.T) {
	assign := &mutationsunversioned.Assign{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: mutationsunversioned.AssignSpec{
			ApplyTo: []match.MutationApplyTo{
				{ApplyTo: match.ApplyTo{
					Groups:   []string{"group3", "group4"},
					Kinds:    []string{"kind4", "kind2", "kind3"},
					Versions: []string{"version1"},
				}},
			},
			Match:    match.Match{},
			Location: "aaa..bb",
			Parameters: mutationsunversioned.Parameters{
				Assign: makeValue("foobar"),
			},
		},
	}

	_, err := MutatorForAssign(assign)
	if err == nil || !strings.Contains(err.Error(), "invalid location format") {
		t.Errorf("Parsing was expected to fail for assign: %v", err)
	}

	assignMeta := &mutationsunversioned.AssignMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: mutationsunversioned.AssignMetadataSpec{
			Match:    match.Match{},
			Location: "spec...foo",
			Parameters: mutationsunversioned.MetadataParameters{
				Assign: makeValue("foobar"),
			},
		},
	}
	_, err = MutatorForAssignMetadata(assignMeta)
	if err == nil || !strings.Contains(err.Error(), "invalid location format") {
		t.Errorf("Parsing was expected to fail for assign metadata: %v", err)
	}
}

func TestPathValidation(t *testing.T) {
	assignMeta := &mutationsunversioned.AssignMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: mutationsunversioned.AssignMetadataSpec{
			Match: match.Match{},
			Parameters: mutationsunversioned.MetadataParameters{
				Assign: makeValue("foobar"),
			},
		},
	}

	table := []struct {
		tname    string
		location string
		isValid  bool
	}{
		{
			"validlabel",
			"metadata.labels.mutate",
			true,
		},
		{
			"validannotation",
			"metadata.annotations.mutate",
			true,
		},
		{
			"changename",
			"metadata.name",
			false,
		},
		{
			"containers",
			"spec.containers[name: foo]",
			false,
		},
	}

	for _, tc := range table {
		t.Run(tc.tname, func(t *testing.T) {
			a := assignMeta.DeepCopy()
			a.Spec.Location = tc.location
			_, err := MutatorForAssignMetadata(a)

			if tc.isValid && err != nil {
				t.Errorf("Unexpected error for location %s, %v", tc.location, err)
			}
			if !tc.isValid && (err == nil || !strings.HasPrefix(err.Error(), "invalid location")) {
				t.Errorf("Location was invalid but did not get an invalid location error, %v", err)
			}
		})
	}
}
