package constraint

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Error represents a single error caught while adding a constraint to OPA
type Error struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Location string `json:"location,omitempty"`
}

// ByPodStatus defines the observed state of a constraint as seen by
// an individual controller
type ByPodStatus struct {
	// a unique identifier for the pod that wrote the status
	ID                 string  `json:"id,omitempty"`
	ObservedGeneration int64   `json:"observedGeneration,omitempty"`
	Errors             []Error `json:"errors,omitempty"`
	Enforced           bool    `json:"enforced,omitempty"`
}

func GetHAStatus(obj *unstructured.Unstructured) (*ByPodStatus, error) {
	s, err := getHAStatus(obj)
	if err != nil {
		return nil, err
	}
	j, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	status := &ByPodStatus{}
	if err := json.Unmarshal(j, status); err != nil {
		return nil, err
	}
	return status, nil
}

func SetHAStatus(obj *unstructured.Unstructured, status *ByPodStatus) error {
	j, err := json.Marshal(status)
	if err != nil {
		return err
	}
	var s map[string]interface{}
	if err := json.Unmarshal(j, &s); err != nil {
		return err
	}
	return setHAStatus(obj, s)
}

func DeleteHAStatus(obj *unstructured.Unstructured) error {
	return deleteHAStatus(obj)
}
