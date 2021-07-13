package mutators

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestAssignToMutator(t *testing.T) {
	assign := &mutationsv1alpha1.Assign{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: mutationsv1alpha1.AssignSpec{
			ApplyTo: []match.ApplyTo{
				{
					Groups:   []string{"group1", "group2"},
					Kinds:    []string{"kind1", "kind2", "kind3"},
					Versions: []string{"version1"},
				},
				{
					Groups:   []string{"group3", "group4"},
					Kinds:    []string{"kind4", "kind2", "kind3"},
					Versions: []string{"version1"},
				},
			},
			Match:    match.Match{},
			Location: "spec.foo",
			Parameters: mutationsv1alpha1.Parameters{
				Assign: runtime.RawExtension{Raw: []byte(`{"value": "foobar"}`)},
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
	assignMeta := &mutationsv1alpha1.AssignMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: mutationsv1alpha1.AssignMetadataSpec{
			Match:    match.Match{},
			Location: "metadata.labels.foo",
			Parameters: mutationsv1alpha1.MetadataParameters{
				Assign: runtime.RawExtension{Raw: []byte(`{"value": "foobar"}`)},
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
	first := &mutationsv1alpha1.Assign{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: mutationsv1alpha1.AssignSpec{
			ApplyTo: []match.ApplyTo{
				{
					Groups:   []string{"group1", "group2"},
					Kinds:    []string{"kind1", "kind2", "kind3"},
					Versions: []string{"version1"},
				},
				{
					Groups:   []string{"group3", "group4"},
					Kinds:    []string{"kind4", "kind2", "kind3"},
					Versions: []string{"version1"},
				},
			},
			Match:    match.Match{},
			Location: "spec.foo",
			Parameters: mutationsv1alpha1.Parameters{
				Assign: runtime.RawExtension{Raw: []byte(`{"value": "foobar"}`)},
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
		modify       func(*mutationsv1alpha1.Assign)
		areDifferent bool
	}{
		{
			"same",
			func(a *mutationsv1alpha1.Assign) {},
			false,
		},
		{
			"differentApplyTo",
			func(a *mutationsv1alpha1.Assign) {
				a.Spec.ApplyTo[1].Kinds[0] = "kind"
			},
			true,
		},
		{
			"differentLocation",
			func(a *mutationsv1alpha1.Assign) {
				a.Spec.Location = "bar"
			},
			true,
		},
		{
			"differentParameters",
			func(a *mutationsv1alpha1.Assign) {
				a.Spec.Parameters.AssignIf = runtime.RawExtension{Raw: []byte(`{"in": ["Foo","Bar"]}`)}
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
	first := &mutationsv1alpha1.AssignMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: mutationsv1alpha1.AssignMetadataSpec{
			Match:    match.Match{},
			Location: "metadata.labels.foo",
			Parameters: mutationsv1alpha1.MetadataParameters{
				Assign: runtime.RawExtension{Raw: []byte(`{"value": "foobar"}`)},
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
		modify       func(*mutationsv1alpha1.AssignMetadata)
		areDifferent bool
	}{
		{
			"same",
			func(a *mutationsv1alpha1.AssignMetadata) {},
			false,
		},
		{
			"differentLocation",
			func(a *mutationsv1alpha1.AssignMetadata) {
				a.Spec.Location = "metadata.annotations.bar"
			},
			true,
		},
		{
			"differentName",
			func(a *mutationsv1alpha1.AssignMetadata) {
				a.Name = "anothername"
			},
			true,
		},
		{
			"differentMatch",
			func(a *mutationsv1alpha1.AssignMetadata) {
				a.Spec.Match.Namespaces = []string{"foo", "bar"}
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
	assign := &mutationsv1alpha1.Assign{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: mutationsv1alpha1.AssignSpec{
			ApplyTo: []match.ApplyTo{
				{
					Groups:   []string{"group3", "group4"},
					Kinds:    []string{"kind4", "kind2", "kind3"},
					Versions: []string{"version1"},
				},
			},
			Match:    match.Match{},
			Location: "aaa..bb",
			Parameters: mutationsv1alpha1.Parameters{
				Assign: runtime.RawExtension{Raw: []byte(`{"value": "foobar"}`)},
			},
		},
	}

	_, err := MutatorForAssign(assign)
	if err == nil || !strings.Contains(err.Error(), "invalid location format") {
		t.Errorf("Parsing was expected to fail for assign: %v", err)
	}

	assignMeta := &mutationsv1alpha1.AssignMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: mutationsv1alpha1.AssignMetadataSpec{
			Match:    match.Match{},
			Location: "spec...foo",
			Parameters: mutationsv1alpha1.MetadataParameters{
				Assign: runtime.RawExtension{Raw: []byte(`{"value": "foobar"}`)},
			},
		},
	}
	_, err = MutatorForAssignMetadata(assignMeta)
	if err == nil || !strings.Contains(err.Error(), "invalid location format") {
		t.Errorf("Parsing was expected to fail for assign metadata: %v", err)
	}
}

func TestPathValidation(t *testing.T) {
	assignMeta := &mutationsv1alpha1.AssignMetadata{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
		Spec: mutationsv1alpha1.AssignMetadataSpec{
			Match: match.Match{},
			Parameters: mutationsv1alpha1.MetadataParameters{
				Assign: runtime.RawExtension{Raw: []byte(`{"value": "foobar"}`)},
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
