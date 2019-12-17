package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"github.com/pkg/errors"
	types "k8s.io/api/admission/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func init() {
	AddToManagerFuncs = append(AddToManagerFuncs, AddLabelWebhook)
}

const ignoreLabel = "gatekeeper-ignore"

// +kubebuilder:webhook:verbs=CREATE;UPDATE,path=/v1/admitlabel,mutating=false,failurePolicy=fail,groups="",resources=namespaces,versions=*,name=check-ignore-label.gatekeeper.sh

// AddLabelWebhook registers the label webhook server with the manager
func AddLabelWebhook(mgr manager.Manager, _ *opa.Client) error {
	wh := &admission.Webhook{Handler: &namespaceLabelHandler{}}
	mgr.GetWebhookServer().Register("/v1/admitlabel", wh)
	return nil
}

var _ admission.Handler = &namespaceLabelHandler{}

type namespaceLabelHandler struct{}

func (h *namespaceLabelHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	if req.Operation == types.Delete {
		return admission.Allowed("Delete is always allowed")
	}
	if req.AdmissionRequest.Kind.Group != "" || req.AdmissionRequest.Kind.Kind != "Namespace" {
		return admission.Allowed("Not a namespace")
	}
	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(req.Object.Raw, obj); err != nil {
		r := admission.Denied(errors.Wrap(err, "while deserializing resource").Error())
		r.Result.Code = http.StatusInternalServerError
		return r
	}
	if obj.GetName() == util.GetNamespace() {
		return admission.Allowed(fmt.Sprintf("Gatekeeper namespace is allowed to set the %s label", ignoreLabel))
	}
	for label := range obj.GetLabels() {
		if label == ignoreLabel {
			return admission.Denied(fmt.Sprintf("Only Gatekeeper's namespace can have the %s label", ignoreLabel))
		}
	}
	return admission.Allowed(fmt.Sprintf("Namespace is not setting the %s label", ignoreLabel))
}
