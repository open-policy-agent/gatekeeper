package testhelpers

import (
	"reflect"

	externaldatav1alpha1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/match"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/mutators/core"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	path "github.com/open-policy-agent/gatekeeper/pkg/mutation/path/tester"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func (d *DummyMutator) Value() (interface{}, error) {
	return d.value, nil
}

func (d *DummyMutator) Path() parser.Path {
	return d.path
}

func (d *DummyMutator) Matches(obj client.Object, ns *corev1.Namespace) bool {
	matches, err := match.Matches(&d.match, obj, ns)
	if err != nil {
		return false
	}
	return matches
}

func (d *DummyMutator) Mutate(obj *unstructured.Unstructured, providerResponseCache map[types.ProviderCacheKey]string) (bool, error) {
	t, _ := path.New(parser.Path{}, nil)
	return core.Mutate(d, t, func(_ interface{}, _ bool) bool { return true }, obj, nil)
}

func (d *DummyMutator) String() string {
	return ""
}

func (d *DummyMutator) GetExternalDataProvider() string {
	return ""
}

func (d *DummyMutator) GetExternalDataSource() types.DataSource {
	return ""
}

func (d *DummyMutator) GetExternalDataCache(name string) (*externaldatav1alpha1.Provider, error) {
	return &externaldatav1alpha1.Provider{}, nil
}

func NewDummyMutator(name, path string, value interface{}) *DummyMutator {
	p, err := parser.Parse(path)
	if err != nil {
		panic(err)
	}
	return &DummyMutator{name: name, path: p, value: value}
}
