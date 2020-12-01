package mutation

import (
	"github.com/google/go-cmp/cmp"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// AssignMutator is a mutator object built out of a
// Assign instance.
type AssignMutator struct {
	id       ID
	assign   *mutationsv1alpha1.Assign
	path     *parser.Path
	bindings []SchemaBinding
}

// AssignMutator implements mutatorWithSchema
var _ MutatorWithSchema = &AssignMutator{}

func (m *AssignMutator) Matches(obj runtime.Object, ns *corev1.Namespace) bool {
	matches, err := Matches(m.assign.Spec.Match, obj, ns)
	if err != nil {
		log.Error(err, "AssignMutator.Matches failed", "assign", m.assign.Name)
		return false
	}
	return matches
}

func (m *AssignMutator) Mutate(obj *unstructured.Unstructured) error {
	// TODO implement
	return nil
}
func (m *AssignMutator) ID() ID {
	return m.id
}

func (m *AssignMutator) SchemaBindings() []SchemaBinding {
	return m.bindings
}

func (m *AssignMutator) HasDiff(mutator Mutator) bool {
	toCheck, ok := mutator.(*AssignMutator)
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

func (m *AssignMutator) Path() *parser.Path {
	return m.path
}

func (m *AssignMutator) DeepCopy() Mutator {
	res := &AssignMutator{
		id:     m.id,
		assign: m.assign.DeepCopy(),
		path: &parser.Path{
			Nodes: make([]parser.Node, len(m.path.Nodes)),
		},
		bindings: make([]SchemaBinding, len(m.bindings)),
	}
	copy(res.path.Nodes, m.path.Nodes)
	copy(res.bindings, m.bindings)
	return res
}

// MutatorForAssign returns an AssignMutator built from
// the given assign instance.
func MutatorForAssign(assign *mutationsv1alpha1.Assign) (*AssignMutator, error) {
	id, err := MakeID(assign)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to retrieve id for assign type")
	}

	path, err := parser.Parse(assign.Spec.Location)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to parse the location specified")
	}

	return &AssignMutator{
		id:       id,
		assign:   assign.DeepCopy(),
		bindings: applyToToBindings(assign.Spec.ApplyTo),
		path:     path,
	}, nil
}

func applyToToBindings(applyTos []mutationsv1alpha1.ApplyTo) []SchemaBinding {
	res := []SchemaBinding{}
	for _, applyTo := range applyTos {
		binding := SchemaBinding{
			Groups:   make([]string, len(applyTo.Groups)),
			Kinds:    make([]string, len(applyTo.Kinds)),
			Versions: make([]string, len(applyTo.Versions)),
		}
		for i, g := range applyTo.Groups {
			binding.Groups[i] = g
		}
		for i, k := range applyTo.Kinds {
			binding.Kinds[i] = k
		}
		for i, v := range applyTo.Versions {
			binding.Versions[i] = v
		}
		res = append(res, binding)
	}
	return res
}
