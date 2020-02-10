package constraint

import (
	"fmt"

	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func blankStatus(id string, generation int64) map[string]interface{} {
	return map[string]interface{}{
		"id":                 id,
		"observedGeneration": generation,
	}
}

// getHAStatus gets the value of a pod-specific subfield of status
func getHAStatus(obj *unstructured.Unstructured) (map[string]interface{}, error) {
	id := util.GetID()
	gen := obj.GetGeneration()
	statuses, exists, err := unstructured.NestedSlice(obj.Object, "status", "byPod")
	if err != nil {
		return nil, errors.Wrap(err, "while getting HA status")
	}
	if !exists {
		return blankStatus(id, gen), nil
	}

	for _, s := range statuses {
		status, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		curID2, ok := status["id"]
		if !ok {
			continue
		}
		curID, ok := curID2.(string)
		if !ok {
			continue
		}
		if id == curID {
			return status, nil
		}
	}

	return blankStatus(id, gen), nil
}

// setHAStatus sets the value of a pod-specific subfield of status
func setHAStatus(obj *unstructured.Unstructured, status map[string]interface{}) error {
	id := util.GetID()
	status["id"] = id
	status["observedGeneration"] = obj.GetGeneration()
	statuses, exists, err := unstructured.NestedSlice(obj.Object, "status", "byPod")
	if err != nil {
		return errors.Wrap(err, "while setting HA status")
	}
	if !exists {
		if err := unstructured.SetNestedSlice(
			obj.Object, []interface{}{status}, "status", "byPod"); err != nil {
			return errors.Wrap(err, "while setting HA status")
		}
	}

	for i, s := range statuses {
		curStatus, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		curID2, ok := curStatus["id"]
		if !ok {
			continue
		}
		curID, ok := curID2.(string)
		if !ok {
			continue
		}
		if id == curID {
			statuses[i] = status
			if err := unstructured.SetNestedSlice(
				obj.Object, statuses, "status", "byPod"); err != nil {
				return errors.Wrap(err, "while setting HA status")
			}
			return nil
		}
	}

	statuses = append(statuses, status)
	if err := unstructured.SetNestedSlice(
		obj.Object, statuses, "status", "byPod"); err != nil {
		return errors.Wrap(err, "while setting HA status")
	}
	return nil
}

func deleteHAStatus(obj *unstructured.Unstructured) error {
	id := util.GetID()

	statuses, exists, err := unstructured.NestedSlice(obj.Object, "status", "byPod")
	if err != nil {
		return errors.Wrap(err, "while deleting HA status")
	}
	if !exists {
		return nil
	}

	newStatus := make([]interface{}, 0)

	for i, s := range statuses {
		curStatus, ok := s.(map[string]interface{})
		if !ok {
			return fmt.Errorf("element %d in byPod status is malformed", i)
		}
		curID2, ok := curStatus["id"]
		if !ok {
			return fmt.Errorf("element %d in byPod status is missing an `id` field", i)
		}
		curID, ok := curID2.(string)
		if !ok {
			return fmt.Errorf("element %d in byPod status' `id` field is not a string: %v", i, curID2)
		}
		if id == curID {
			continue
		}
		newStatus = append(newStatus, s)
	}
	if err := unstructured.SetNestedSlice(obj.Object, newStatus, "status", "byPod"); err != nil {
		return errors.Wrap(err, "while writing deleted byPod status")
	}
	return nil
}
