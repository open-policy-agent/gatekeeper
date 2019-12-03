package util

import (
	"fmt"
	"os"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/open-policy-agent/gatekeeper/api/v1alpha1"
)

func getID() string {
	return os.Getenv("POD_NAME")
}

func blankStatus(id string) map[string]interface{} {
	return map[string]interface{}{
		"id": id,
	}
}

func GetCTHAStatus(template *v1beta1.ConstraintTemplate) *v1beta1.ByPodStatus {
	id := getID()
	for _, status := range template.Status.ByPod {
		if status.ID == id {
			return status
		}
	}
	return &v1beta1.ByPodStatus{ID: id}
}

func SetCTHAStatus(template *v1beta1.ConstraintTemplate, status *v1beta1.ByPodStatus) {
	id := getID()
	status.ID = id
	for i, s := range template.Status.ByPod {
		if s.ID == id {
			template.Status.ByPod[i] = status
			return
		}
	}
	template.Status.ByPod = append(template.Status.ByPod, status)
}

func DeleteCTHAStatus(template *v1beta1.ConstraintTemplate) {
	id := getID()
	var newStatus []*v1beta1.ByPodStatus
	for _, status := range template.Status.ByPod {
		if status.ID == id {
			continue
		}
		newStatus = append(newStatus, status)
	}
	template.Status.ByPod = newStatus
}

func GetCfgHAStatus(cfg *v1alpha1.Config) *v1alpha1.ByPod {
	id := getID()
	for _, status := range cfg.Status.ByPod {
		if status.ID == id {
			return status
		}
	}
	return &v1alpha1.ByPod{ID: id}
}

func SetCfgHAStatus(cfg *v1alpha1.Config, status *v1alpha1.ByPod) {
	id := getID()
	status.ID = id
	for i, s := range cfg.Status.ByPod {
		if s.ID == id {
			cfg.Status.ByPod[i] = status
			return
		}
	}
	cfg.Status.ByPod = append(cfg.Status.ByPod, status)
}

func DeleteCfgHAStatus(cfg *v1alpha1.Config) {
	id := getID()
	var newStatus []*v1alpha1.ByPod
	for _, status := range cfg.Status.ByPod {
		if status.ID == id {
			continue
		}
		newStatus = append(newStatus, status)
	}
	cfg.Status.ByPod = newStatus
}

// GetHAStatus gets the value of a pod-specific subfield of status
func GetHAStatus(obj *unstructured.Unstructured) (map[string]interface{}, error) {
	id := getID()
	statuses, exists, err := unstructured.NestedSlice(obj.Object, "status", "byPod")
	if err != nil {
		return nil, errors.Wrap(err, "while getting HA status")
	}
	if !exists {
		return blankStatus(id), nil
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

	return blankStatus(id), nil
}

// SetHAStatus sets the value of a pod-specific subfield of status
func SetHAStatus(obj *unstructured.Unstructured, status map[string]interface{}) error {
	id := getID()
	status["id"] = id
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

func DeleteHAStatus(obj *unstructured.Unstructured) error {
	id := getID()

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
