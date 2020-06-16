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
	"strings"

	"github.com/open-policy-agent/gatekeeper/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	ConstraintNameLabel = "internal.gatekeeper.sh/constraint-name"
	ConstraintKindLabel = "internal.gatekeeper.sh/constraint-kind"
	PodLabel            = "internal.gatekeeper.sh/pod"
	ConstraintsGroup    = "constraints.gatekeeper.sh"
)

// ConstraintPodStatusStatus defines the observed state of ConstraintPodStatus
type ConstraintPodStatusStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	ID string `json:"id,omitempty"`
	// Storing the constraint UID allows us to detect drift, such as
	// when a constraint has been recreated after its CRD was deleted
	// out from under it, interrupting the watch
	ConstraintUID      types.UID `json:"constraintUID,omitempty"`
	Operations         []string  `json:"operations,omitempty"`
	Enforced           bool      `json:"enforced,omitempty"`
	Errors             []Error   `json:"errors,omitempty"`
	ObservedGeneration int64     `json:"observedGeneration,omitempty"`
}

// Error represents a single error caught while adding a constraint to OPA
type Error struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Location string `json:"location,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced

// ConstraintPodStatus is the Schema for the constraintpodstatuses API
type ConstraintPodStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status ConstraintPodStatusStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ConstraintPodStatusList contains a list of ConstraintPodStatus
type ConstraintPodStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConstraintPodStatus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ConstraintPodStatus{}, &ConstraintPodStatusList{})
}

// NewConstraintStatusForPod returns a constraint status object
// that has been initialized with the bare minimum of fields to make it functional
// with the constraint status controller
func NewConstraintStatusForPod(pod *corev1.Pod, constraint *unstructured.Unstructured, scheme *runtime.Scheme) (*ConstraintPodStatus, error) {
	obj := &ConstraintPodStatus{}
	name, err := KeyForConstraint(pod.Name, constraint)
	if err != nil {
		return nil, err
	}
	obj.SetName(name)
	obj.SetNamespace(util.GetNamespace())
	obj.Status.ID = pod.Name
	obj.Status.Operations = operations.AssignedStringList()
	obj.SetLabels(map[string]string{
		ConstraintNameLabel: constraint.GetName(),
		ConstraintKindLabel: constraint.GetKind(),
		PodLabel:            pod.Name,
		// the template name is the lower-case of the constraint kind
		ConstraintTemplateNameLabel: strings.ToLower(constraint.GetKind()),
	})
	if PodOwnershipEnabled() {
		if err := controllerutil.SetOwnerReference(pod, obj, scheme); err != nil {
			return nil, err
		}
	}
	return obj, nil
}

// KeyForConstraint returns a unique status object name given the Pod ID and
// a constraint object
func KeyForConstraint(id string, constraint *unstructured.Unstructured) (string, error) {
	// We don't need to worry that lower-casing the kind will cause a collision because
	// the constraint framework requires resource == lower-case kind. We must do this
	// because K8s requires all lowercase letters for resource names
	kind := strings.ToLower(constraint.GetObjectKind().GroupVersionKind().Kind)
	name := constraint.GetName()
	return dashPacker(id, kind, name)
}
