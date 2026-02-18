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

// ConfigPodStatusStatus defines the observed state of ConfigPodStatus.

// +kubebuilder:object:generate=true

type ConfigPodStatusStatus struct {
	ID                 string         `json:"id,omitempty"`
	ConfigUID          types.UID      `json:"configUID,omitempty"`
	Operations         []string       `json:"operations,omitempty"`
	ObservedGeneration int64          `json:"observedGeneration,omitempty"`
	Errors             []*ConfigError `json:"errors,omitempty"`
}

// +kubebuilder:object:generate=true

type ConfigError struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message"`
}

// ConfigPodStatus is the Schema for the configpodstatuses API.

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced

type ConfigPodStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status ConfigPodStatusStatus `json:"status,omitempty"`
}

// ConfigPodStatusList contains a list of ConfigPodStatus.

// +kubebuilder:object:root=true
type ConfigPodStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConfigPodStatus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ConfigPodStatus{}, &ConfigPodStatusList{})
}

// NewConfigStatusForPod returns an config status object
// that has been initialized with the bare minimum of fields to make it functional
// with the config status controller.
func NewConfigStatusForPod(pod *corev1.Pod, configNamespace string, configName string, scheme *runtime.Scheme) (*ConfigPodStatus, error) {
	obj := &ConfigPodStatus{}
	name, err := KeyForConfig(pod.Name, configNamespace, configName)
	if err != nil {
		return nil, err
	}
	obj.SetName(name)
	obj.SetNamespace(util.GetNamespace())
	obj.Status.ID = pod.Name
	obj.Status.Operations = operations.AssignedStringList()
	obj.SetLabels(map[string]string{
		ConfigNameLabel: configName,
		PodLabel:        pod.Name,
	})

	// Skip OwnerReference in remote cluster mode
	if !util.ShouldSkipPodOwnerRef() {
		if err := controllerutil.SetOwnerReference(pod, obj, scheme); err != nil {
			return nil, err
		}
	}

	return obj, nil
}

// KeyForConfig returns a unique status object name given the Pod ID and
// a config object.
// The object name must satisfy RFC 1123 Label Names spec
// (https://kubernetes.io/docs/concepts/overview/working-with-objects/names/)
// and Kubernetes validation rules for object names.
//
// It's possible that dash packing/unpacking would result in a name
// that exceeds the maximum length allowed, but for Config resources,
// the configName should always be "config", and namespace would be "gatekeeper-system",
// so this validation will hold.
func KeyForConfig(id string, configNamespace string, configName string) (string, error) {
	return DashPacker(id, configNamespace, configName)
}
