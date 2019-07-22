package webhook

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	templv1alpha1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1alpha1"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	rtypes "github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/pkg/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/config"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission/builder"
	atypes "sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
)

func init() {
	CreateWebhookFuncs = append(CreateWebhookFuncs, AddValidatingWebhook)
}

// AddValidatingWebhook creates the validating webhook
// below: notations add permissions kube-mgmt needs. Access cannot yet be restricted on a namespace-level granularity
// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
// +kubebuilder:rbac:groups=,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
func AddValidatingWebhook(mgr manager.Manager, opa opa.Client) (webhook.Webhook, error) {
	validatingWH, err := builder.NewWebhookBuilder().
		Validating().
		Name(*validatingWHName).
		Path("/v1/admit").
		Rules(admissionregistrationv1beta1.RuleWithOperations{
			Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update},
			Rule: admissionregistrationv1beta1.Rule{
				APIGroups:   []string{"*"},
				APIVersions: []string{"*"},
				Resources:   []string{"*"},
			},
		}).
		Handlers(&validationHandler{opa: opa, k8s: mgr.GetClient()}).
		WithManager(mgr).
		Build()

	if err != nil {
		return nil, err
	}

	return validatingWH, nil
}

var _ admission.Handler = &validationHandler{}

type validationHandler struct {
	opa    opa.Client
	k8s client.Client

	// for testing
	injectedConfig *v1alpha1.Config
}

// Handle the validation request
func (h *validationHandler) Handle(ctx context.Context, req atypes.Request) atypes.Response {
	log := log.WithValues("hookType", "validation")
	if isGkServiceAccount(req.AdmissionRequest.UserInfo) {
		return admission.ValidationResponse(true, "Gatekeeper does not self-manage")
	}

	if req.AdmissionRequest.Operation == admissionv1beta1.Delete {
		// oldObject is the existing object.
		// It is null for DELETE operations in API servers prior to v1.15.0.
		// https://github.com/kubernetes/website/pull/14671
		if req.AdmissionRequest.OldObject.Raw == nil {
			vResp := admission.ValidationResponse(false, "For admission webhooks registered for DELETE operations, please use Kubernetes v1.15.0+.")
			vResp.Response.Result.Code = http.StatusInternalServerError
			return vResp
		} else {
			// For admission webhooks registered for DELETE operations on k8s built APIs or CRDs,
			// the apiserver now sends the existing object as admissionRequest.Request.OldObject to the webhook
			// object is the new object being admitted.
			// It is null for DELETE operations.
			// https://github.com/kubernetes/kubernetes/pull/76346
			req.AdmissionRequest.Object = req.AdmissionRequest.OldObject
		}
	}

	if userErr, err := h.validateGatekeeperResources(ctx, req); err != nil {
		vResp := admission.ValidationResponse(false, err.Error())
		if vResp.Response.Result == nil {
			vResp.Response.Result = &metav1.Status{}
		}
		if userErr {
			vResp.Response.Result.Code = http.StatusUnprocessableEntity
		} else {
			vResp.Response.Result.Code = http.StatusInternalServerError
		}
		return vResp
	}

	resp, err := h.reviewRequest(ctx, req)
	if err != nil {
		log.Error(err, "error executing query")
		vResp := admission.ValidationResponse(false, err.Error())
		if vResp.Response.Result == nil {
			vResp.Response.Result = &metav1.Status{}
		}
		vResp.Response.Result.Code = http.StatusInternalServerError
		return vResp
	}

	res := resp.Results()
	if len(res) != 0 {
		var msgs []string
		for _, r := range res {
			msgs = append(msgs, fmt.Sprintf("[denied by %s] %s", r.Constraint.GetName(), r.Msg))
		}
		vResp := admission.ValidationResponse(false, strings.Join(msgs, "\n"))
		if vResp.Response.Result == nil {
			vResp.Response.Result = &metav1.Status{}
		}
		vResp.Response.Result.Code = http.StatusForbidden
		return vResp
	}

	return admission.ValidationResponse(true, "")
}

func (h *validationHandler) getConfig(ctx context.Context) (*v1alpha1.Config, error) {
	if h.injectedConfig != nil {
		return h.injectedConfig, nil
	}
	if h.k8s == nil {
		return nil, errors.New("no client available to retrieve validation config")
	}
	cfg := &v1alpha1.Config{}
	return cfg, h.k8s.Get(ctx, config.CfgKey, cfg)
}

func isGkServiceAccount(user authenticationv1.UserInfo) bool {
	saGroup := fmt.Sprintf("system:serviceaccounts:%s", namespace)
	for _, g := range user.Groups {
		if g == saGroup {
			return true
		}
	}
	return false
}

// validateGatekeeperResources returns whether an issue is user error (vs internal) and any errors
// validating internal resources
func (h *validationHandler) validateGatekeeperResources(ctx context.Context, req atypes.Request) (bool, error) {
	if req.AdmissionRequest.Kind.Group == "templates.gatekeeper.sh" && req.AdmissionRequest.Kind.Kind == "ConstraintTemplate" {
		return h.validateTemplate(ctx, req)
	}
	if req.AdmissionRequest.Kind.Group == "constraints.gatekeeper.sh" {
		return h.validateConstraint(ctx, req)
	}
	return false, nil
}

func (h *validationHandler) validateTemplate(ctx context.Context, req atypes.Request) (bool, error) {
	templ := &templv1alpha1.ConstraintTemplate{}
	if _, _, err := deserializer.Decode(req.AdmissionRequest.Object.Raw, nil, templ); err != nil {
		return false, err
	}
	if _, err := h.opa.CreateCRD(ctx, templ); err != nil {
		return true, err
	}
	return false, nil
}

func (h *validationHandler) validateConstraint(ctx context.Context, req atypes.Request) (bool, error) {
	obj := &unstructured.Unstructured{}
	if _, _, err := deserializer.Decode(req.AdmissionRequest.Object.Raw, nil, obj); err != nil {
		return false, err
	}
	if err := h.opa.ValidateConstraint(ctx, obj); err != nil {
		return true, err
	}
	return false, nil
}

// traceSwitch returns true if a request should be traced
func (h *validationHandler) reviewRequest(ctx context.Context, req atypes.Request) (*rtypes.Responses, error) {
	cfg, _ := h.getConfig(ctx)
	traceEnabled := false
	dump := false
	for _, trace := range cfg.Spec.Validation.Traces {
		if trace.User != req.AdmissionRequest.UserInfo.Username {
			continue
		}
		gvk := v1alpha1.GVK{
			Group:   req.AdmissionRequest.Kind.Group,
			Version: req.AdmissionRequest.Kind.Version,
			Kind:    req.AdmissionRequest.Kind.Kind,
		}
		if gvk == trace.Kind {
			traceEnabled = true
			if trace.Dump == "All" {
				dump = true
			}
		}
	}

	resp, err := h.opa.Review(ctx, req.AdmissionRequest, opa.Tracing(traceEnabled))
	if traceEnabled {
		log.Info(resp.TraceDump())
	}
	if dump {
		dump, err := h.opa.Dump(ctx)
		if err != nil {
			log.Error(err, "dump error")
		} else {
			log.Info(dump)
		}
	}
	return resp, err
}
