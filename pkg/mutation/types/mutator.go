package types

import (
	"encoding/json"
	"fmt"

	externaldatav1alpha1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ID represent the identifier of a mutation object.
type ID struct {
	Group     string
	Kind      string
	Namespace string
	Name      string
}

type DataSource string

const (
	ValueAtLocation DataSource = "valueAtLocation"
	Username        DataSource = "username"
)

// ProviderCacheKey is the map key for provider requests
type ProviderCacheKey struct {
	ProviderName string `json:"providerName,omitempty"`
	// outbound data is based on Provider DataSource
	// it is either value of location or username in admission request
	OutboundData string     `json:"outboundData,omitempty"`
	DataSource   DataSource `json:"dataSource,omitempty"`
}

func (k ProviderCacheKey) MarshalText() ([]byte, error) {
	type p ProviderCacheKey
	return json.Marshal(p(k))
}

func (k *ProviderCacheKey) UnmarshalText(text []byte) error {
	type x ProviderCacheKey
	return json.Unmarshal(text, (*x)(k))
}

func (id ID) String() string {
	return fmt.Sprintf("%v %v",
		schema.GroupKind{Group: id.Group, Kind: id.Kind},
		client.ObjectKey{Namespace: id.Namespace, Name: id.Name})
}

// Mutator represent a mutation object.
type Mutator interface {
	// Matches tells if the given object is eligible for this mutation.
	Matches(obj client.Object, ns *corev1.Namespace) bool
	// Mutate applies the mutation to the given object
	Mutate(obj *unstructured.Unstructured, providerResponseCache map[ProviderCacheKey]string) (bool, error)
	// ID returns the id of the current mutator.
	ID() ID
	// Has diff tells if the mutator has meaningful differences
	// with the provided mutator
	HasDiff(mutator Mutator) bool
	// DeepCopy returns a copy of the current object
	DeepCopy() Mutator
	Value() (interface{}, error)
	Path() parser.Path
	String() string
	GetExternalDataProvider() string
	GetExternalDataSource() DataSource
	GetExternalDataCache(name string) (*externaldatav1alpha1.Provider, error)
}

// MakeID builds an ID object for the given object.
func MakeID(obj client.Object) ID {
	return ID{
		Group:     obj.GetObjectKind().GroupVersionKind().Group,
		Kind:      obj.GetObjectKind().GroupVersionKind().Kind,
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}
}

// UnmarshalValue unmarshals the value a mutation is meant to assign.
func UnmarshalValue(data []byte) (interface{}, error) {
	value := make(map[string]interface{})
	err := json.Unmarshal(data, &value)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to unmarshal value %s", data)
	}
	return value["value"], nil
}
