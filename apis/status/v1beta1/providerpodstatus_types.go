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
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ExternalDataGroup is the API Group for Gatekeeper External Data Providers.
const ExternalDataGroup = "externaldata.gatekeeper.sh"

// ProviderPodStatusStatus defines the observed state of ProviderPodStatus.
type ProviderPodStatusStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	ID string `json:"id,omitempty"`
	// Storing the provider UID allows us to detect drift, such as
	// when a provider has been recreated after its CRD was deleted
	// out from under it, interrupting the watch
	ProviderUID            types.UID        `json:"providerUID,omitempty"`
	Operations             []string         `json:"operations,omitempty"`
	Active                 bool             `json:"active,omitempty"`
	Errors                 []ProviderError  `json:"errors,omitempty"`
	ObservedGeneration     int64            `json:"observedGeneration,omitempty"`
	LastTransitionTime     *metav1.Time     `json:"lastTransitionTime,omitempty"`
	LastCacheUpdateTime    *metav1.Time     `json:"lastCacheUpdateTime,omitempty"`
}

// ProviderError represents a single error caught while managing providers.
type ProviderError struct {
	// Type indicates a specific class of error for use by controller code.
	// If not present, the error should be treated as not matching any known type.
	Type           ProviderErrorType `json:"type,omitempty"`
	Message        string            `json:"message"`
	Retryable      bool              `json:"retryable,omitempty"`
	ErrorTimestamp *metav1.Time      `json:"errorTimestamp,omitempty"`
}

// ProviderErrorType represents different types of provider errors.
type ProviderErrorType string

const (
	// ConversionError indicates an error converting provider configuration.
	ConversionError ProviderErrorType = "conversion_error"
	// UpsertCacheError indicates an error updating the provider cache.
	UpsertCacheError ProviderErrorType = "upsert_cache_error"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced

// ProviderPodStatus is the Schema for the providerpodstatuses API.
type ProviderPodStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status ProviderPodStatusStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProviderPodStatusList contains a list of ProviderPodStatus.
type ProviderPodStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderPodStatus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ProviderPodStatus{}, &ProviderPodStatusList{})
}

// NewProviderStatusForPod returns a provider status object
// that has been initialized with the bare minimum of fields to make it functional
// with the provider status controller.
func NewProviderStatusForPod(pod *corev1.Pod, providerName string, providerUID types.UID, scheme *runtime.Scheme) (*ProviderPodStatus, error) {
	obj := &ProviderPodStatus{}
	name := KeyForProvider(pod.Name, providerName)
	obj.SetName(name)
	obj.SetNamespace(util.GetNamespace())
	obj.Status.ID = pod.Name
	obj.Status.ProviderUID = providerUID
	obj.Status.Operations = operations.AssignedStringList()
	if err := controllerutil.SetOwnerReference(pod, obj, scheme); err != nil {
		return nil, err
	}
	return obj, nil
}

// KeyForProvider generates a unique key for a ProviderPodStatus object.
func KeyForProvider(podName, providerName string) string {
	return podName + "-" + providerName
}