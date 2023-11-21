package core

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/parser"
	patht "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/tester"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// NewTester returns a path.Tester for the given object name, kind path and
// pathtests.
func NewTester(name string, kind string, path parser.Path, ptests []unversioned.PathTest) (*patht.Tester, error) {
	pathTests, err := gatherPathTests(name, kind, ptests)
	if err != nil {
		return nil, err
	}
	tester, err := patht.New(path, pathTests)
	if err != nil {
		return nil, err
	}

	return tester, nil
}

// NewValidatedBindings returns a slice of gvks from the given applies, or an
// error if the applies are invalid.
func NewValidatedBindings(name string, kind string, applies []match.ApplyTo) ([]schema.GroupVersionKind, error) {
	for _, applyTo := range applies {
		if len(applyTo.Groups) == 0 || len(applyTo.Versions) == 0 || len(applyTo.Kinds) == 0 {
			return nil, fmt.Errorf("invalid applyTo for %s mutator %s, all of group, version and kind must be specified", kind, name)
		}
	}

	gvks := getSortedGVKs(applies)
	if len(gvks) == 0 {
		return nil, fmt.Errorf("applyTo required for %s mutator %s", kind, name)
	}

	return gvks, nil
}

func gatherPathTests(mutName string, mutKind string, pts []unversioned.PathTest) ([]patht.Test, error) {
	var pathTests []patht.Test
	for _, pt := range pts {
		p, err := parser.Parse(pt.SubPath)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("problem parsing sub path `%s` for %s %s", pt.SubPath, mutKind, mutName))
		}
		pathTests = append(pathTests, patht.Test{SubPath: p, Condition: pt.Condition})
	}

	return pathTests, nil
}

func getSortedGVKs(bindings []match.ApplyTo) []schema.GroupVersionKind {
	// deduplicate GVKs
	gvksMap := map[schema.GroupVersionKind]struct{}{}
	for _, binding := range bindings {
		for _, gvk := range binding.Flatten() {
			gvksMap[gvk] = struct{}{}
		}
	}

	var gvks []schema.GroupVersionKind
	for gvk := range gvksMap {
		gvks = append(gvks, gvk)
	}

	// we iterate over the map in a stable order so that
	// unit tests won't be flaky.
	sort.Slice(gvks, func(i, j int) bool { return gvks[i].String() < gvks[j].String() })

	return gvks
}

// HasMetadataRoot returns true if the root node at given path references the
// metadata field.
func HasMetadataRoot(path parser.Path) bool {
	if len(path.Nodes) == 0 {
		return false
	}
	return reflect.DeepEqual(path.Nodes[0], &parser.Object{Reference: "metadata"})
}

// CheckKeyNotChanged does not allow to change the key field of
// a list element. A path like foo[name: bar].name is rejected.
func CheckKeyNotChanged(p parser.Path) error {
	if len(p.Nodes) == 0 {
		return errors.New("empty path")
	}
	if len(p.Nodes) < 2 {
		return nil
	}
	lastNode := p.Nodes[len(p.Nodes)-1]
	secondLastNode := p.Nodes[len(p.Nodes)-2]

	if secondLastNode.Type() != parser.ListNode {
		return nil
	}
	if lastNode.Type() != parser.ObjectNode {
		return fmt.Errorf("invalid path format: child of a list can't be a list")
	}
	addedObject, ok := lastNode.(*parser.Object)
	if !ok {
		return errors.New("failed converting an ObjectNodeType to Object")
	}
	listNode, ok := secondLastNode.(*parser.List)
	if !ok {
		return errors.New("failed converting a ListNodeType to List")
	}

	if addedObject.Reference == listNode.KeyField {
		return fmt.Errorf("invalid path format: changing the item key is not allowed")
	}

	return nil
}

func MatchWithApplyTo(mut *types.Mutable, applies []match.ApplyTo, mat *match.Match) (bool, error) {
	gvk := mut.Object.GetObjectKind().GroupVersionKind()
	if !match.AppliesTo(applies, gvk) {
		return false, nil
	}

	target := &match.Matchable{
		Object:    mut.Object,
		Namespace: mut.Namespace,
		Source:    mut.Source,
	}
	matches, err := match.Matches(mat, target)
	if err != nil {
		return false, err
	}

	return matches, nil
}

func ValidateName(name string) error {
	if len(name) > 63 {
		return ErrNameLength
	}

	return nil
}
