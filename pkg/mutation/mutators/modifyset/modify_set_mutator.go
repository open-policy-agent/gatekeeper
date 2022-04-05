package modifyset

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/google/go-cmp/cmp"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/apis/mutations/unversioned"
	mutationsv1beta1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1beta1"
	"github.com/open-policy-agent/gatekeeper/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/core"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	patht "github.com/open-policy-agent/gatekeeper/pkg/mutation/path/tester"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/schema"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	runtimeschema "k8s.io/apimachinery/pkg/runtime/schema"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("mutation").WithValues(logging.Process, "mutation", logging.Mutator, "modifyset")

// Mutator is a mutator object built out of a
// ModifySet instance.
type Mutator struct {
	id        types.ID
	modifySet *mutationsunversioned.ModifySet

	path parser.Path

	// bindings are the set of GVKs this Mutator applies to.
	bindings []runtimeschema.GroupVersionKind
	tester   *patht.Tester
}

// Mutator implements mutatorWithSchema.
var _ schema.MutatorWithSchema = &Mutator{}

func (m *Mutator) Matches(mutable *types.Mutable) bool {
	gvk := mutable.Object.GetObjectKind().GroupVersionKind()
	if !match.AppliesTo(m.modifySet.Spec.ApplyTo, gvk) {
		return false
	}
	matches, err := match.Matches(&m.modifySet.Spec.Match, mutable.Object, mutable.Namespace)
	if err != nil {
		log.Error(err, "Matches failed for modify set", "modifyset", m.modifySet.Name)
		return false
	}
	return matches
}

func (m *Mutator) TerminalType() parser.NodeType {
	return schema.Set
}

func (m *Mutator) Mutate(mutable *types.Mutable) (bool, error) {
	values := m.modifySet.Spec.Parameters.Values.DeepCopy().FromList

	return core.Mutate(
		m.Path(),
		m.tester,
		setter{
			op:     m.modifySet.Spec.Parameters.Operation,
			values: values,
		},
		mutable.Object,
	)
}

func (m *Mutator) UsesExternalData() bool {
	// modify set doesn't use external data
	return false
}

func (m *Mutator) ID() types.ID {
	return m.id
}

func (m *Mutator) SchemaBindings() []runtimeschema.GroupVersionKind {
	return m.bindings
}

func (m *Mutator) HasDiff(mutator types.Mutator) bool {
	toCheck, ok := mutator.(*Mutator)
	if !ok { // different types, different
		return true
	}

	if !cmp.Equal(toCheck.id, m.id) {
		return true
	}
	if !cmp.Equal(toCheck.path, m.path) {
		return true
	}
	if !cmp.Equal(toCheck.bindings, m.bindings) {
		return true
	}

	// any difference in spec may be enough
	if !cmp.Equal(toCheck.modifySet.Spec, m.modifySet.Spec) {
		return true
	}

	return false
}

func (m *Mutator) Path() parser.Path {
	return m.path
}

func (m *Mutator) DeepCopy() types.Mutator {
	res := &Mutator{
		id:        m.id,
		modifySet: m.modifySet.DeepCopy(),
		path: parser.Path{
			Nodes: make([]parser.Node, len(m.path.Nodes)),
		},
		bindings: make([]runtimeschema.GroupVersionKind, len(m.bindings)),
	}

	copy(res.path.Nodes, m.path.Nodes)
	copy(res.bindings, m.bindings)
	res.tester = m.tester.DeepCopy()
	return res
}

func (m *Mutator) String() string {
	return fmt.Sprintf("%s/%s/%s:%d", m.id.Kind, m.id.Namespace, m.id.Name, m.modifySet.GetGeneration())
}

// MutatorForModifySet returns an Mutator built from
// the given modifyset instance.
func MutatorForModifySet(modifySet *mutationsunversioned.ModifySet) (*Mutator, error) {
	// This is not always set by the kubernetes API server
	modifySet.SetGroupVersionKind(runtimeschema.GroupVersionKind{Group: mutationsv1beta1.GroupVersion.Group, Kind: "ModifySet"})

	path, err := parser.Parse(modifySet.Spec.Location)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid location format `%s` for ModifySet %s", modifySet.Spec.Location, modifySet.GetName())
	}

	if hasMetadataRoot(path) {
		return nil, fmt.Errorf("modifyset %s can't change metadata", modifySet.GetName())
	}

	if path.Nodes[len(path.Nodes)-1].Type() == parser.ListNode {
		return nil, fmt.Errorf("final node in a modifyset location cannot be a keyed list")
	}

	id := types.MakeID(modifySet)

	pathTests, err := gatherPathTests(modifySet)
	if err != nil {
		return nil, err
	}
	tester, err := patht.New(path, pathTests)
	if err != nil {
		return nil, err
	}
	for _, applyTo := range modifySet.Spec.ApplyTo {
		if len(applyTo.Groups) == 0 || len(applyTo.Versions) == 0 || len(applyTo.Kinds) == 0 {
			return nil, fmt.Errorf("invalid applyTo for ModifySet mutator %s, all of group, version and kind must be specified", modifySet.GetName())
		}
	}

	gvks := getSortedGVKs(modifySet.Spec.ApplyTo)
	if len(gvks) == 0 {
		return nil, fmt.Errorf("applyTo required for ModifySet mutator %s", modifySet.GetName())
	}

	return &Mutator{
		id:        id,
		modifySet: modifySet.DeepCopy(),
		bindings:  gvks,
		path:      path,
		tester:    tester,
	}, nil
}

func gatherPathTests(modifySet *mutationsunversioned.ModifySet) ([]patht.Test, error) {
	pts := modifySet.Spec.Parameters.PathTests
	var pathTests []patht.Test
	for _, pt := range pts {
		p, err := parser.Parse(pt.SubPath)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("problem parsing sub path `%s` for ModifySet %s", pt.SubPath, modifySet.GetName()))
		}
		pathTests = append(pathTests, patht.Test{SubPath: p, Condition: pt.Condition})
	}
	return pathTests, nil
}

// IsValidModifySet returns an error if the given modifyset object is not
// semantically valid.
func IsValidModifySet(modifySet *mutationsunversioned.ModifySet) error {
	if _, err := MutatorForModifySet(modifySet); err != nil {
		return err
	}
	return nil
}

func hasMetadataRoot(path parser.Path) bool {
	if len(path.Nodes) == 0 {
		return false
	}

	if reflect.DeepEqual(path.Nodes[0], &parser.Object{Reference: "metadata"}) {
		return true
	}
	return false
}

func getSortedGVKs(bindings []match.ApplyTo) []runtimeschema.GroupVersionKind {
	// deduplicate GVKs
	gvksMap := map[runtimeschema.GroupVersionKind]struct{}{}
	for _, binding := range bindings {
		for _, gvk := range binding.Flatten() {
			gvksMap[gvk] = struct{}{}
		}
	}

	var gvks []runtimeschema.GroupVersionKind
	for gvk := range gvksMap {
		gvks = append(gvks, gvk)
	}

	// we iterate over the map in a stable order so that
	// unit tests won't be flaky.
	sort.Slice(gvks, func(i, j int) bool { return gvks[i].String() < gvks[j].String() })

	return gvks
}

var _ core.Setter = setter{}

type setter struct {
	values []interface{}
	op     mutationsunversioned.Operation
}

func (s setter) KeyedListOkay() bool { return false }

func (s setter) KeyedListValue() (map[string]interface{}, error) {
	panic("modifyset setter does not handle keyed lists")
}

func (s setter) SetValue(obj map[string]interface{}, key string) error {
	switch s.op {
	case mutationsunversioned.MergeOp:
		return s.setValueMerge(obj, key)
	case mutationsunversioned.PruneOp:
		return s.setValuePrune(obj, key)
	default:
		return fmt.Errorf("unrecognized operation for modifyset: %s", s.op)
	}
}

func (s setter) setValueMerge(obj map[string]interface{}, key string) error {
	val, ok := obj[key]
	// missing list => add all values as a new list.
	if !ok {
		obj[key] = runtime.DeepCopyJSONValue(s.values)
		return nil
	}

	vals, ok := val.([]interface{})
	if !ok {
		return fmt.Errorf("%+v is not a list of values, cannot treat it as a set", val)
	}
outer:
	for _, v := range s.values {
		for _, existing := range vals {
			if cmp.Equal(v, existing) {
				continue outer
			}
		}
		// Value does not currently exist, add it.
		vals = append(vals, v)
	}
	obj[key] = vals
	return nil
}

func (s setter) setValuePrune(obj map[string]interface{}, key string) error {
	val, ok := obj[key]
	// missing list => we're done.
	if !ok {
		return nil
	}

	vals, ok := val.([]interface{})
	if !ok {
		return fmt.Errorf("%+v is not a list of values, cannot treat it as a set", val)
	}

	// we are assuming order is important, otherwise this could be done
	// more cheaply by swapping values
	filtered := make([]interface{}, 0, len(vals))
	for _, existing := range vals {
		matched := false
		for _, v := range s.values {
			if cmp.Equal(v, existing) {
				matched = true
			}
		}
		if !matched {
			filtered = append(filtered, existing)
		}
	}
	obj[key] = filtered
	return nil
}
