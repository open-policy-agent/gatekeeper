package assign

import (
	"fmt"
	"reflect"

	"github.com/google/go-cmp/cmp"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	mutationsv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/core"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/parser"
	patht "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/tester"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/schema"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/pkg/errors"
	runtimeschema "k8s.io/apimachinery/pkg/runtime/schema"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("mutation").WithValues(logging.Process, "mutation", logging.Mutator, "assign")

// Mutator is a mutator object built out of an
// Assign instance.
type Mutator struct {
	id     types.ID
	assign *mutationsunversioned.Assign

	path parser.Path

	// bindings are the set of GVKs this Mutator applies to.
	bindings []runtimeschema.GroupVersionKind
	tester   *patht.Tester
}

// Mutator implements mutatorWithSchema.
var _ schema.MutatorWithSchema = &Mutator{}

func (m *Mutator) Matches(mutable *types.Mutable) (bool, error) {
	res, err := core.MatchWithApplyTo(mutable, m.assign.Spec.ApplyTo, &m.assign.Spec.Match)
	if err != nil {
		log.Error(err, "Matches failed for assign", "assign", m.assign.Name)
	}
	return res, err
}

func (m *Mutator) TerminalType() parser.NodeType {
	return schema.Unknown
}

func (m *Mutator) Mutate(mutable *types.Mutable) (bool, error) {
	value, err := m.assign.Spec.Parameters.Assign.GetValue(mutable)
	if err != nil {
		return false, err
	}
	return core.Mutate(m.Path(), m.tester, core.NewDefaultSetter(value), mutable.Object)
}

func (m *Mutator) MustTerminate() bool {
	return m.assign.Spec.Parameters.Assign.ExternalData != nil
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
	if !cmp.Equal(toCheck.assign.Spec, m.assign.Spec) {
		return true
	}

	return false
}

func (m *Mutator) Path() parser.Path {
	return m.path
}

func (m *Mutator) DeepCopy() types.Mutator {
	res := &Mutator{
		id:     m.id,
		assign: m.assign.DeepCopy(),
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
	return fmt.Sprintf("%s/%s/%s:%d", m.id.Kind, m.id.Namespace, m.id.Name, m.assign.GetGeneration())
}

// MutatorForAssign returns a mutator built from the given assign instance.
func MutatorForAssign(assign *mutationsunversioned.Assign) (*Mutator, error) {
	if err := core.ValidateName(assign.Name); err != nil {
		return nil, err
	}
	// This is not always set by the kubernetes API server
	assign.SetGroupVersionKind(runtimeschema.GroupVersionKind{Group: mutationsv1beta1.GroupVersion.Group, Kind: "Assign"})

	path, err := parser.Parse(assign.Spec.Location)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid location format `%s` for Assign %s", assign.Spec.Location, assign.GetName())
	}

	if hasMetadataRoot(path) {
		return nil, fmt.Errorf("assign %s can't change metadata", assign.GetName())
	}

	err = core.CheckKeyNotChanged(path)
	if err != nil {
		return nil, err
	}

	potentialValues := assign.Spec.Parameters.Assign
	if err := potentialValues.Validate(); err != nil {
		return nil, err
	}

	if potentialValues.Value != nil {
		if err = validateObjectAssignedToList(path, potentialValues.Value.GetValue()); err != nil {
			return nil, err
		}
	}

	if path.Nodes[len(path.Nodes)-1].Type() == parser.ListNode {
		if potentialValues.FromMetadata != nil {
			return nil, errors.New("cannot assign a metadata field to a list")
		}
		if potentialValues.ExternalData != nil {
			return nil, errors.New("cannot assign external data response to a list")
		}
	}

	tester, err := core.NewTester(assign.GetName(), "Assign", path, assign.Spec.Parameters.PathTests)
	if err != nil {
		return nil, err
	}
	gvks, err := core.NewValidatedBindings(assign.GetName(), "Assign", assign.Spec.ApplyTo)
	if err != nil {
		return nil, err
	}

	return &Mutator{
		id:       types.MakeID(assign),
		assign:   assign.DeepCopy(),
		bindings: gvks,
		path:     path,
		tester:   tester,
	}, nil
}

// IsValidAssign returns an error if the given assign object is not
// semantically valid.
func IsValidAssign(assign *mutationsunversioned.Assign) error {
	if _, err := MutatorForAssign(assign); err != nil {
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

func validateObjectAssignedToList(p parser.Path, value interface{}) error {
	if len(p.Nodes) == 0 {
		return errors.New("empty path")
	}
	if p.Nodes[len(p.Nodes)-1].Type() != parser.ListNode {
		return nil
	}
	listNode, ok := p.Nodes[len(p.Nodes)-1].(*parser.List)
	if !ok {
		return errors.New("failed converting a ListNodeType to List")
	}
	if listNode.Glob {
		return errors.New("can't append to a globbed list")
	}
	if listNode.KeyValue == nil {
		return errors.New("invalid key value for a non globbed object")
	}
	valueMap, ok := value.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid value: `%+v`, only objects can be added to keyed lists", value)
	}
	if listNode.KeyValue != valueMap[listNode.KeyField] {
		return fmt.Errorf("adding object to list with different key %s: list key %v, object key %v", listNode.KeyField, listNode.KeyValue, valueMap[listNode.KeyField])
	}

	return nil
}
