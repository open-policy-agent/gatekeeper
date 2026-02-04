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

package v1alpha1

import (
	"github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ConnectionPodStatusStatus defines the observed state of ConnectionPodStatus.
type ConnectionPodStatusStatus struct {
	// ID is the unique identifier for the pod that wrote the status
	ID                 string    `json:"id,omitempty"`
	ConnectionUID      types.UID `json:"connectionUID,omitempty"`
	Operations         []string  `json:"operations,omitempty"`
	ObservedGeneration int64     `json:"observedGeneration,omitempty"`
	// Indicator for alive connection with at least one successful publish
	Active bool               `json:"active,omitempty"`
	Errors []*ConnectionError `json:"errors,omitempty"`
}

type ConnectionError struct {
	Type    connectionErrorType `json:"type"`
	Message string              `json:"message"`
}

type connectionErrorType string

const (
	UpsertConnectionError connectionErrorType = "UpsertConnection"
	PublishError          connectionErrorType = "Publish"
)

// +kubebuilder:object:root=true
// ConnectionPodStatus is the Schema for the connectionpodstatuses API.
type ConnectionPodStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// No spec field is defined here, as this is a status-only resource.
	Status ConnectionPodStatusStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// ConnectionPodStatusList contains a list of ConnectionPodStatus.
type ConnectionPodStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConnectionPodStatus `json:"items"`
}

// NewConnectionStatusForPod returns a connection status object
// that has been initialized with the bare minimum of fields to make it functional
// with the connection status controller.
func NewConnectionStatusForPod(pod *corev1.Pod, connectionNamespace, connectionName string, scheme *runtime.Scheme) (*ConnectionPodStatus, error) {
	obj := &ConnectionPodStatus{}
	name, err := KeyForConnection(pod.Name, connectionNamespace, connectionName)
	if err != nil {
		return nil, err
	}
	obj.SetName(name)
	obj.SetNamespace(util.GetNamespace())
	obj.Status.ID = pod.Name
	obj.Status.Operations = operations.AssignedStringList()
	obj.SetLabels(map[string]string{
		v1beta1.ConnectionNameLabel: connectionName,
		v1beta1.PodLabel:            pod.Name,
	})

	// Skip OwnerReference in external mode
	if !util.ShouldSkipPodOwnerRef() {
		if err := controllerutil.SetOwnerReference(pod, obj, scheme); err != nil {
			return nil, err
		}
	}

	return obj, nil
}

// KeyForConnection returns a unique status object name given the Pod ID and a connection object.
func KeyForConnection(id string, connectionNamespace string, connectionName string) (string, error) {
	return v1beta1.DashPacker(id, connectionNamespace, connectionName)
}

func init() {
	SchemeBuilder.Register(&ConnectionPodStatus{}, &ConnectionPodStatusList{})
}
