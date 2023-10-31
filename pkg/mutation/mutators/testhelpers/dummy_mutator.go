package testhelpers

import (
	"reflect"
	"strings"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/mutators/core"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/parser"
	path "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/path/tester"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
)

var _ types.Mutator = &DummyMutator{}

// DummyMutator is a blank mutator that makes it easier to test the core mutation function.
type DummyMutator struct {
	name  string
	value interface{}
	path  parser.Path
	match match.Match
}

func (d *DummyMutator) DeepCopy() types.Mutator {
	return d
}

func (d *DummyMutator) HasDiff(m types.Mutator) bool {
	return !reflect.DeepEqual(d, m)
}

func (d *DummyMutator) ID() types.ID {
	return types.ID{Group: "mutators.gatekeeper.sh", Kind: "DummyMutator", Name: d.name}
}

func (d *DummyMutator) Path() parser.Path {
	return d.path
}

func (d *DummyMutator) Matches(mutable *types.Mutable) (bool, error) {
	m := &match.Matchable{Object: mutable.Object, Namespace: mutable.Namespace}
	matches, err := match.Matches(&d.match, m)
	if err != nil {
		return false, err
	}
	return matches, nil
}

func (d *DummyMutator) Mutate(mutable *types.Mutable) (bool, error) {
	t, _ := path.New(parser.Path{}, nil)
	return core.Mutate(d.Path(), t, core.NewDefaultSetter(d.value), mutable.Object)
}

func (d *DummyMutator) MustTerminate() bool {
	return false
}

func (d *DummyMutator) String() string {
	return ""
}

func NewDummyMutator(name, path string, value interface{}) *DummyMutator {
	p, err := parser.Parse(path)
	if err != nil {
		panic(err)
	}
	return &DummyMutator{name: name, path: p, value: value}
}

// BigName returns a 64-length string.
func BigName() string {
	return strings.Repeat("abigname", 8) // 8 X 8 = 64
}
