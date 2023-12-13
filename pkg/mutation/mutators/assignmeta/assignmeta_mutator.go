package assignmeta

import (
	"fmt"
	"reflect"

	"github.com/google/go-cmp/cmp"
	mutationsunversioned "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/unversioned"
	mutationsv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/core"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/parser"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/tester"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/pkg/errors"
	runtimeschema "k8s.io/apimachinery/pkg/runtime/schema"
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
	assignMetadata *mutationsunversioned.AssignMetadata

	path parser.Path

	tester *tester.Tester
}

// Mutator implements mutator.
var _ types.Mutator = &Mutator{}

func (m *Mutator) Matches(mutable *types.Mutable) (bool, error) {
	target := &match.Matchable{
		Object:    mutable.Object,
		Namespace: mutable.Namespace,
		Source:    mutable.Source,
	}
	matches, err := match.Matches(&m.assignMetadata.Spec.Match, target)
	if err != nil {
		log.Error(err, "Matches failed for assign metadata", "assignMeta", m.assignMetadata.Name)
	}
	return matches, err
}

func (m *Mutator) Mutate(mutable *types.Mutable) (bool, error) {
	// Note: Performance here can be improved by ~3x by writing a specialized
	// function instead of using a generic function. AssignMetadata only ever
	// mutates metadata.annotations or metadata.labels, and we spend ~70% of
	// compute covering cases that aren't valid for this Mutator.
	value, err := m.assignMetadata.Spec.Parameters.Assign.GetValue(mutable)
	if err != nil {
		return false, err
	}
	return core.Mutate(m.path, m.tester, core.NewDefaultSetter(value), mutable.Object)
}

func (m *Mutator) MustTerminate() bool {
	return m.assignMetadata.Spec.Parameters.Assign.ExternalData != nil
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
		path:           m.path.DeepCopy(),
		tester:         m.tester.DeepCopy(),
	}
	return res
}

func (m *Mutator) String() string {
	return fmt.Sprintf("%s/%s/%s:%d", m.id.Kind, m.id.Namespace, m.id.Name, m.assignMetadata.GetGeneration())
}

// MutatorForAssignMetadata builds a Mutator from the given AssignMetadata object.
func MutatorForAssignMetadata(assignMeta *mutationsunversioned.AssignMetadata) (*Mutator, error) {
	if err := core.ValidateName(assignMeta.Name); err != nil {
		return nil, err
	}

	// This is not always set by the kubernetes API server
	assignMeta.SetGroupVersionKind(runtimeschema.GroupVersionKind{Group: mutationsv1beta1.GroupVersion.Group, Kind: "AssignMetadata"})

	path, err := parser.Parse(assignMeta.Spec.Location)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid location format for AssignMetadata %s: %s", assignMeta.GetName(), assignMeta.Spec.Location)
	}
	if !isValidMetadataPath(path) {
		return nil, fmt.Errorf("invalid location for assignmetadata %s: %s", assignMeta.GetName(), assignMeta.Spec.Location)
	}

	potentialValue := assignMeta.Spec.Parameters.Assign
	if err := potentialValue.Validate(); err != nil {
		return nil, err
	}
	if potentialValue.ExternalData != nil && potentialValue.ExternalData.DataSource != types.DataSourceUsername {
		return nil, fmt.Errorf("only username data source is supported for assignmetadata %s", assignMeta.GetName())
	}

	if potentialValue.Value != nil {
		if _, ok := potentialValue.Value.GetValue().(string); !ok {
			return nil, fmt.Errorf("spec.parameters.assign.value field must be a string for AssignMetadata %q", assignMeta.GetName())
		}
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
func IsValidAssignMetadata(assignMeta *mutationsunversioned.AssignMetadata) error {
	if _, err := MutatorForAssignMetadata(assignMeta); err != nil {
		return err
	}
	return nil
}
