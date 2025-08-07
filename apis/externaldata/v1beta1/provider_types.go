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
	status "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	ByPod []status.ProviderPodStatusStatus `json:"byPod,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// Provider is the Schema for the providers API.
type Provider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the Provider specifications.
	Spec   ProviderSpec   `json:"spec,omitempty"`
	Status ProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProviderList contains a list of Provider.
type ProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items contains the list of Providers.
	Items []Provider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Provider{}, &ProviderList{})
}