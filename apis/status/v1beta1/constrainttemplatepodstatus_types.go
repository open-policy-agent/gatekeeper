/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	templatesv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ConstraintTemplatePodStatusStatus defines the observed state of ConstraintTemplatePodStatus.
type ConstraintTemplatePodStatusStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// id is the name of the pod that generated this status.
	ID string `json:"id,omitempty"`
	// templateUID is the UID of the ConstraintTemplate this status reports on, used
	// to detect drift, such as when the ConstraintTemplate has been recreated after
	// its CRD was deleted out from under it, interrupting the watch.
	TemplateUID types.UID `json:"templateUID,omitempty"`
	// operations lists the Gatekeeper operations assigned to the pod that generated
	// this status.
	Operations []string `json:"operations,omitempty"`
	// observedGeneration is the generation of the ConstraintTemplate that was last
	// processed by this pod.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// errors lists any errors encountered while creating the CRD for the
	// ConstraintTemplate on this pod.
	Errors []*templatesv1beta1.CreateCRDError `json:"errors,omitempty"`
	// vapGenerationStatus reports the status of generating a ValidatingAdmissionPolicy
	// for the ConstraintTemplate on this pod.
	VAPGenerationStatus *VAPGenerationStatus `json:"vapGenerationStatus,omitempty"`
}

// VAPGenerationStatus represents the status of VAP generation.
type VAPGenerationStatus struct {
	// state is the current state of ValidatingAdmissionPolicy generation.
	State string `json:"state,omitempty"`
	// observedGeneration is the generation of the ConstraintTemplate that was last
	// processed for ValidatingAdmissionPolicy generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// warning contains a human-readable warning encountered while generating the
	// ValidatingAdmissionPolicy, if any.
	Warning string `json:"warning,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced

// ConstraintTemplatePodStatus is the Schema for the constrainttemplatepodstatuses API.
type ConstraintTemplatePodStatus struct {
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard object metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// status is the observed state of the ConstraintTemplate for this pod.
	Status ConstraintTemplatePodStatusStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ConstraintTemplatePodStatusList contains a list of ConstraintTemplatePodStatus.
type ConstraintTemplatePodStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConstraintTemplatePodStatus `json:"items"`
}

// NewConstraintTemplateStatusForPod returns a constraint template status object
// that has been initialized with the bare minimum of fields to make it functional
// with the constraint template status controller.
func NewConstraintTemplateStatusForPod(pod *corev1.Pod, templateName string, scheme *runtime.Scheme) (*ConstraintTemplatePodStatus, error) {
	obj := &ConstraintTemplatePodStatus{}
	name, err := KeyForConstraintTemplate(pod.Name, templateName)
	if err != nil {
		return nil, err
	}
	obj.SetName(name)
	obj.SetNamespace(util.GetNamespace())
	obj.Status.ID = pod.Name
	obj.Status.Operations = operations.AssignedStringList()
	obj.SetLabels(map[string]string{
		ConstraintTemplateNameLabel: templateName,
		PodLabel:                    pod.Name,
	})

	if err := controllerutil.SetOwnerReference(pod, obj, scheme); err != nil {
		return nil, err
	}

	return obj, nil
}

// KeyForConstraintTemplate returns a unique status object name given the Pod ID and
// a template object.
func KeyForConstraintTemplate(id string, templateName string) (string, error) {
	return DashPacker(id, templateName)
}
