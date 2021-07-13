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

	mtypes "github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// MutationsGroup is the API Group for Gatekeeper Mutators.
const MutationsGroup = "mutations.gatekeeper.sh"

// MutatorPodStatusStatus defines the observed state of MutatorPodStatus.
type MutatorPodStatusStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	ID string `json:"id,omitempty"`
	// Storing the mutator UID allows us to detect drift, such as
	// when a mutator has been recreated after its CRD was deleted
	// out from under it, interrupting the watch
	MutatorUID         types.UID      `json:"mutatorUID,omitempty"`
	Operations         []string       `json:"operations,omitempty"`
	Enforced           bool           `json:"enforced,omitempty"`
	Errors             []MutatorError `json:"errors,omitempty"`
	ObservedGeneration int64          `json:"observedGeneration,omitempty"`
}

// MutatorError represents a single error caught while adding a mutator to a system.
type MutatorError struct {
	Message string `json:"message"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced

// MutatorPodStatus is the Schema for the mutationpodstatuses API.
type MutatorPodStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status MutatorPodStatusStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MutatorPodStatusList contains a list of MutatorPodStatus.
type MutatorPodStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MutatorPodStatus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MutatorPodStatus{}, &MutatorPodStatusList{})
}

// NewMutatorStatusForPod returns a mutator status object
// that has been initialized with the bare minimum of fields to make it functional
// with the mutator status controller.
func NewMutatorStatusForPod(pod *corev1.Pod, mutatorID mtypes.ID, scheme *runtime.Scheme) (*MutatorPodStatus, error) {
	obj := &MutatorPodStatus{}
	name, err := KeyForMutatorID(pod.Name, mutatorID)
	if err != nil {
		return nil, err
	}
	obj.SetName(name)
	obj.SetNamespace(util.GetNamespace())
	obj.Status.ID = pod.Name
	obj.Status.Operations = operations.AssignedStringList()

	obj.SetLabels(map[string]string{
		MutatorNameLabel: mutatorID.Name,
		MutatorKindLabel: mutatorID.Kind,
		PodLabel:         pod.Name,
	})
	if PodOwnershipEnabled() {
		if err := controllerutil.SetOwnerReference(pod, obj, scheme); err != nil {
			return nil, err
		}
	}
	return obj, nil
}

// KeyForMutatorID returns a unique status object name given the Pod ID and
// a mutator object.
func KeyForMutatorID(id string, mID mtypes.ID) (string, error) {
	// This adds a requirement that the lowercase of all mutator kinds must be unique.
	// Though this should already be the case because resource ~= lower(kind) (usually).
	// We must do this because K8s requires all lowercase letters for resource names
	kind := strings.ToLower(mID.Kind)
	name := mID.Name
	return dashPacker(id, kind, name)
}
