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
	"github.com/open-policy-agent/gatekeeper/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	ConstraintTemplateMapLabel = "internal.gatekeeper.sh/constrainttemplate-map"
)

// ConstraintTemplatePodStatusStatus defines the observed state of ConstraintTemplatePodStatus
type ConstraintTemplatePodStatusStatus struct {
	// Important: Run "make" to regenerate code after modifying this file
	ID                 string                             `json:"id,omitempty"`
	TemplateUID        types.UID                          `json:"templateUID,omitempty"`
	Operations         []string                           `json:"operations,omitempty"`
	ObservedGeneration int64                              `json:"observedGeneration,omitempty"`
	Errors             []*templatesv1beta1.CreateCRDError `json:"errors,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced

// ConstraintTemplatePodStatus is the Schema for the constrainttemplatepodstatuses API
type ConstraintTemplatePodStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status ConstraintTemplatePodStatusStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ConstraintTemplatePodStatusList contains a list of ConstraintTemplatePodStatus
type ConstraintTemplatePodStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConstraintTemplatePodStatus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ConstraintTemplatePodStatus{}, &ConstraintTemplatePodStatusList{})
}

// NewConstraintTemplateStatusForPod returns a constraint template status object
// that has been initialized with the bare minimum of fields to make it functional
// with the constraint template status controller
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
		ConstraintTemplateMapLabel: templateName,
		PodLabel:                   pod.Name,
	})
	if PodOwnershipEnabled() {
		if err := controllerutil.SetOwnerReference(pod, obj, scheme); err != nil {
			return nil, err
		}
	}
	return obj, nil
}

// KeyForConstraintTemplate returns a unique status object name given the Pod ID and
// a template object
func KeyForConstraintTemplate(id string, templateName string) (string, error) {
	return dashPacker(id, templateName)
}
