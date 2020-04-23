package util

import (
	"os"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
)

// GetID returns a unique name for the Gatekeeper pod
func GetID() string {
	return os.Getenv("POD_NAME")
}

func GetCTHAStatus(template *v1beta1.ConstraintTemplate) *v1beta1.ByPodStatus {
	id := GetID()
	for _, status := range template.Status.ByPod {
		if status.ID == id {
			return status
		}
	}
	return &v1beta1.ByPodStatus{
		ID:                 id,
		ObservedGeneration: template.GetGeneration(),
	}
}

func SetCTHAStatus(template *v1beta1.ConstraintTemplate, status *v1beta1.ByPodStatus) {
	id := GetID()
	status.ID = id
	status.ObservedGeneration = template.GetGeneration()
	for i, s := range template.Status.ByPod {
		if s.ID == id {
			template.Status.ByPod[i] = status
			return
		}
	}
	template.Status.ByPod = append(template.Status.ByPod, status)
}

func DeleteCTHAStatus(template *v1beta1.ConstraintTemplate) {
	id := GetID()
	var newStatus []*v1beta1.ByPodStatus
	for _, status := range template.Status.ByPod {
		if status.ID == id {
			continue
		}
		newStatus = append(newStatus, status)
	}
	template.Status.ByPod = newStatus
}
