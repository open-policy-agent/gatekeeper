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

// ExpansionTemplatePodStatusStatus defines the observed state of ExpansionTemplatePodStatus.
type ExpansionTemplatePodStatusStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// id is the name of the pod that generated this status.
	ID string `json:"id,omitempty"`
	// templateUID is the UID of the ExpansionTemplate this status reports on, used
	// to detect drift, such as when the ExpansionTemplate has been recreated after
	// its CRD was deleted out from under it, interrupting the watch.
	TemplateUID types.UID `json:"templateUID,omitempty"`
	// operations lists the Gatekeeper operations assigned to the pod that generated
	// this status.
	Operations []string `json:"operations,omitempty"`
	// observedGeneration is the generation of the ExpansionTemplate that was last
	// processed by this pod.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// errors lists any errors encountered while processing the ExpansionTemplate.
	Errors []*ExpansionTemplateError `json:"errors,omitempty"`
}

// +kubebuilder:object:generate=true

type ExpansionTemplateError struct {
	// type indicates a specific class of error for use by controller code. If not
	// present, the error should be treated as not matching any known type.
	Type string `json:"type,omitempty"`
	// message is a human-readable description of the error.
	Message string `json:"message"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced

// ExpansionTemplatePodStatus is the Schema for the expansiontemplatepodstatuses API.
type ExpansionTemplatePodStatus struct {
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard object metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// status is the observed state of the ExpansionTemplate for this pod.
	Status ExpansionTemplatePodStatusStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ExpansionTemplatePodStatusList contains a list of ExpansionTemplatePodStatus.
type ExpansionTemplatePodStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExpansionTemplatePodStatus `json:"items"`
}

// NewExpansionTemplateStatusForPod returns an expansion template status object
// that has been initialized with the bare minimum of fields to make it functional
// with the expansion template status controller.
func NewExpansionTemplateStatusForPod(pod *corev1.Pod, templateName string, scheme *runtime.Scheme) (*ExpansionTemplatePodStatus, error) {
	obj := &ExpansionTemplatePodStatus{}
	name, err := KeyForExpansionTemplate(pod.Name, templateName)
	if err != nil {
		return nil, err
	}
	obj.SetName(name)
	obj.SetNamespace(util.GetNamespace())
	obj.Status.ID = pod.Name
	obj.Status.Operations = operations.AssignedStringList()
	obj.SetLabels(map[string]string{
		ExpansionTemplateNameLabel: templateName,
		PodLabel:                   pod.Name,
	})

	if err := controllerutil.SetOwnerReference(pod, obj, scheme); err != nil {
		return nil, err
	}

	return obj, nil
}

// KeyForExpansionTemplate returns a unique status object name given the Pod ID and
// a template object.
func KeyForExpansionTemplate(id string, templateName string) (string, error) {
	return DashPacker(id, templateName)
}
