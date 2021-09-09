package assignmeta

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/go-cmp/cmp"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/apis/mutations/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/core"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/tester"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("mutation").WithValues(logging.Process, "mutation", logging.Mutator, "assignmeta")

var (
	labelsValidSubPath = []parser.Node{
		&parser.Object{
			Reference: "metadata",
		},
		&parser.Object{
			Reference: "labels",
		},
	}

	annotationValidSubPath = []parser.Node{
		&parser.Object{
			Reference: "metadata",
		},
		&parser.Object{
			Reference: "annotations",
		},
	}
)

// Mutator is a mutator built out of an
// AssignMeta instance.
type Mutator struct {
	id             types.ID
	assignMetadata *mutationsv1alpha1.AssignMetadata
	assignValue    string

	path parser.Path

	tester *tester.Tester
}

// Mutator implements mutator.
var _ types.Mutator = &Mutator{}

func (m *Mutator) Matches(obj client.Object, ns *corev1.Namespace) bool {
	matches, err := match.Matches(&m.assignMetadata.Spec.Match, obj, ns)
	if err != nil {
		log.Error(err, "Matches failed for assign metadata", "assignMeta", m.assignMetadata.Name)
		return false
	}
	return matches
}

func (m *Mutator) Mutate(obj *unstructured.Unstructured) (bool, error) {
	// Note: Performance here can be improved by ~3x by writing a specialized
	// function instead of using a generic function. AssignMetadata only ever
	// mutates metadata.annotations or metadata.labels, and we spend ~70% of
	// compute covering cases that aren't valid for this Mutator.
	return core.Mutate(m.path, m.tester, nil, core.NewDefaultSetter(m), obj)
}

func (m *Mutator) ID() types.ID {
	return m.id
}

func (m *Mutator) Path() parser.Path {
	return m.path
}

func (m *Mutator) HasDiff(mutator types.Mutator) bool {
	toCheck, ok := mutator.(*Mutator)
	if !ok { // different types, different
		return true
	}

	if !cmp.Equal(toCheck.id, m.id) {
		return true
	}
	// any difference in spec may be enough
	if !cmp.Equal(toCheck.assignMetadata.Spec, m.assignMetadata.Spec) {
		return true
	}

	return false
}

func (m *Mutator) DeepCopy() types.Mutator {
	res := &Mutator{
		id:             m.id,
		assignMetadata: m.assignMetadata.DeepCopy(),
		assignValue:    m.assignValue,
		path:           m.path.DeepCopy(),
		tester:         m.tester.DeepCopy(),
	}
	return res
}

func (m *Mutator) Value() (interface{}, error) {
	return m.assignValue, nil
}

func (m *Mutator) String() string {
	return fmt.Sprintf("%s/%s/%s:%d", m.id.Kind, m.id.Namespace, m.id.Name, m.assignMetadata.GetGeneration())
}

// MutatorForAssignMetadata builds an Mutator from the given AssignMetadata object.
func MutatorForAssignMetadata(assignMeta *mutationsv1alpha1.AssignMetadata) (*Mutator, error) {
	path, err := parser.Parse(assignMeta.Spec.Location)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid location format for AssignMetadata %s: %s", assignMeta.GetName(), assignMeta.Spec.Location)
	}
	if !isValidMetadataPath(path) {
		return nil, fmt.Errorf("invalid location for assignmetadata %s: %s", assignMeta.GetName(), assignMeta.Spec.Location)
	}

	assign := make(map[string]interface{})
	err = json.Unmarshal([]byte(assignMeta.Spec.Parameters.Assign.Raw), &assign)
	if err != nil {
		return nil, errors.Wrap(err, "invalid format for parameters.assign")
	}
	value, ok := assign["value"]
	if !ok {
		return nil, errors.New("spec.parameters.assign must have a string value field for AssignMetadata " + assignMeta.GetName())
	}
	valueString, isString := value.(string)
	if !isString {
		return nil, errors.New("spec.parameters.assign.value field must be a string for AssignMetadata " + assignMeta.GetName())
	}

	t, err := tester.New(path, []tester.Test{
		{SubPath: path, Condition: tester.MustNotExist},
	})
	if err != nil {
		return nil, err
	}

	return &Mutator{
		id:             types.MakeID(assignMeta),
		assignMetadata: assignMeta.DeepCopy(),
		assignValue:    valueString,
		path:           path,
		tester:         t,
	}, nil
}

// Verifies that the given path is valid for metadata.
func isValidMetadataPath(path parser.Path) bool {
	// Path must be metadata.annotations.something or metadata.labels.something
	if len(path.Nodes) != 3 ||
		path.Nodes[0].Type() != parser.ObjectNode ||
		path.Nodes[1].Type() != parser.ObjectNode ||
		path.Nodes[2].Type() != parser.ObjectNode {
		return false
	}

	if reflect.DeepEqual(path.Nodes[0:2], labelsValidSubPath) {
		return true
	}
	if reflect.DeepEqual(path.Nodes[0:2], annotationValidSubPath) {
		return true
	}
	return false
}

// IsValidAssignMetadata returns an error if the given assignmetadata object is not
// semantically valid.
func IsValidAssignMetadata(assignMeta *mutationsv1alpha1.AssignMetadata) error {
	if _, err := MutatorForAssignMetadata(assignMeta); err != nil {
		return err
	}
	return nil
}
