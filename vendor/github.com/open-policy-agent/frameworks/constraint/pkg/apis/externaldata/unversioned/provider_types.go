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

package unversioned

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ProviderSpec defines the desired state of Provider.
type ProviderSpec struct {
	// URL is the url for the provider. URL is prefixed with https://.
	URL string `json:"url,omitempty"`
	// Timeout is the timeout when querying the provider.
	Timeout int `json:"timeout,omitempty"`
	// CABundle is a base64-encoded string that contains the TLS CA bundle in PEM format.
	// It is used to verify the signature of the provider's certificate.
	CABundle string `json:"caBundle,omitempty"`
}

// ProviderStatus defines the observed state of Provider.
type ProviderStatus struct {
	// ByPod is the status of the provider by pod
	ByPod []ProviderPodStatusStatus `json:"byPod,omitempty"`
}

// ExternalDataGroup is the API Group for Gatekeeper External Data Providers.
const ExternalDataGroup = "externaldata.gatekeeper.sh"

// ProviderPodStatusStatus defines the observed state of ProviderPodStatus.
type ProviderPodStatusStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	ID string `json:"id,omitempty"`
	// Storing the provider UID allows us to detect drift, such as
	// when a provider has been recreated after its CRD was deleted
	// out from under it, interrupting the watch
	ProviderUID         types.UID       `json:"providerUID,omitempty"`
	Operations          []string        `json:"operations,omitempty"`
	// Active              bool            `json:"active,omitempty"`
	Errors              []ProviderError `json:"errors,omitempty"`
	ObservedGeneration  int64           `json:"observedGeneration,omitempty"`
	LastTransitionTime  *metav1.Time    `json:"lastTransitionTime,omitempty"`
	LastCacheUpdateTime *metav1.Time    `json:"lastCacheUpdateTime,omitempty"`
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

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:skip

// Provider is the Schema for the providers API
// +k8s:openapi-gen=true
type Provider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the Provider specifications.
	Spec   ProviderSpec   `json:"spec,omitempty"`
	Status ProviderStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ProviderList contains a list of Provider.
type ProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items contains the list of Providers.
	Items []Provider `json:"items"`
}

func (p *Provider) SemanticEqual(other *Provider) bool {
	return reflect.DeepEqual(p.Spec, other.Spec)
}
