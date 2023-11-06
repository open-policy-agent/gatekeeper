package assignimage

import (
	"fmt"

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

var log = logf.Log.WithName("mutation").WithValues(logging.Process, "mutation", logging.Mutator, "assignimage")

// Mutator is a mutator object built out of an AssignImage instance.
type Mutator struct {
	id          types.ID
	assignImage *mutationsunversioned.AssignImage

	path parser.Path

	// bindings are the set of GVKs this Mutator applies to.
	bindings []runtimeschema.GroupVersionKind
	tester   *patht.Tester
}

// Mutator implements mutatorWithSchema.
var _ schema.MutatorWithSchema = &Mutator{}

func (m *Mutator) Matches(mutable *types.Mutable) (bool, error) {
	res, err := core.MatchWithApplyTo(mutable, m.assignImage.Spec.ApplyTo, &m.assignImage.Spec.Match)
	if err != nil {
		log.Error(err, "Matches failed for assign image", "assignImage", m.assignImage.Name)
	}
	return res, err
}

func (m *Mutator) TerminalType() parser.NodeType {
	return schema.String
}

func (m *Mutator) Mutate(mutable *types.Mutable) (bool, error) {
	p := m.assignImage.Spec.Parameters
	s := setter{tag: p.AssignTag, domain: p.AssignDomain, path: p.AssignPath}
	return core.Mutate(m.Path(), m.tester, s, mutable.Object)
}

func (m *Mutator) MustTerminate() bool {
	return true
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
	if !cmp.Equal(toCheck.assignImage.Spec, m.assignImage.Spec) {
		return true
	}

	return false
}

func (m *Mutator) Path() parser.Path {
	return m.path
}

func (m *Mutator) DeepCopy() types.Mutator {
	res := &Mutator{
		id:          m.id,
		assignImage: m.assignImage.DeepCopy(),
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
	return fmt.Sprintf("%s/%s/%s:%d", m.id.Kind, m.id.Namespace, m.id.Name, m.assignImage.GetGeneration())
}

// MutatorForAssignImage returns a mutator built from
// the given assignImage instance.
func MutatorForAssignImage(assignImage *mutationsunversioned.AssignImage) (*Mutator, error) {
	if err := core.ValidateName(assignImage.Name); err != nil {
		return nil, err
	}

	// This is not always set by the kubernetes API server
	assignImage.SetGroupVersionKind(runtimeschema.GroupVersionKind{Group: mutationsv1beta1.GroupVersion.Group, Kind: "AssignImage"})

	path, err := parser.Parse(assignImage.Spec.Location)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid location format `%s` for assignImage %s", assignImage.Spec.Location, assignImage.GetName())
	}

	if core.HasMetadataRoot(path) {
		return nil, newMetadataRootError(assignImage.GetName())
	}

	if hasListTerminal(path) {
		return nil, newListTerminalError(assignImage.GetName())
	}

	err = core.CheckKeyNotChanged(path)
	if err != nil {
		return nil, err
	}

	p := assignImage.Spec.Parameters
	if err := validateImageParts(p.AssignDomain, p.AssignPath, p.AssignTag); err != nil {
		return nil, fmt.Errorf("assignImage %s has invalid parameters: %w", assignImage.GetName(), err)
	}

	tester, err := core.NewTester(assignImage.GetName(), "AssignImage", path, p.PathTests)
	if err != nil {
		return nil, err
	}

	gvks, err := core.NewValidatedBindings(assignImage.GetName(), "AssignImage", assignImage.Spec.ApplyTo)
	if err != nil {
		return nil, err
	}

	return &Mutator{
		id:          types.MakeID(assignImage),
		assignImage: assignImage.DeepCopy(),
		bindings:    gvks,
		path:        path,
		tester:      tester,
	}, nil
}

func hasListTerminal(path parser.Path) bool {
	if len(path.Nodes) == 0 {
		return false
	}
	return path.Nodes[len(path.Nodes)-1].Type() == parser.ListNode
}

var _ core.Setter = setter{}

type setter struct {
	tag    string
	domain string
	path   string
}

func (s setter) KeyedListOkay() bool { return false }

func (s setter) KeyedListValue() (map[string]interface{}, error) {
	panic("assignimage setter does not handle keyed lists")
}

func (s setter) SetValue(obj map[string]interface{}, key string) error {
	val, exists := obj[key]
	strVal := ""
	if exists {
		val, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected value at AssignImage location to be a string, got %v of type %T", val, val)
		}
		strVal = val
	}

	obj[key] = mutateImage(s.domain, s.path, s.tag, strVal)
	return nil
}

// IsValidAssignImage returns an error if the given assignImage object is not
// semantically valid.
func IsValidAssignImage(assignImage *mutationsunversioned.AssignImage) error {
	if _, err := MutatorForAssignImage(assignImage); err != nil {
		return err
	}
	return nil
}
