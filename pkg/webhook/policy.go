package webhook

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"

	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	rtypes "github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/api"
	"github.com/open-policy-agent/gatekeeper/api/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/config"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func init() {
	AddToManagerFuncs = append(AddToManagerFuncs, AddPolicyWebhook)
	api.AddToScheme(runtimeScheme)
}

var log = logf.Log.WithName("webhook")

var (
	runtimeScheme                      = k8sruntime.NewScheme()
	codecs                             = serializer.NewCodecFactory(runtimeScheme)
	deserializer                       = codecs.UniversalDeserializer()
	disableEnforcementActionValidation = flag.Bool("disable-enforcementaction-validation", false, "disable validation of the enforcementAction field of a constraint")
	webhookName                        = flag.String("webhook-name", "validation.gatekeeper.sh", "DEPRECATED: set this on the manifest YAML if needed")
	disableCertRotation                = flag.Bool("disable-cert-rotation", false, "disable automatic generation and rotation of webhook TLS certificates/keys")
)

var supportedEnforcementActions = []string{
	"deny",
	"dryrun",
}

// +kubebuilder:webhook:verbs=create;update,path=/v1/admit,mutating=false,failurePolicy=ignore,groups=*,resources=*,versions=*,name=validation.gatekeeper.sh
// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

// AddPolicyWebhook registers the policy webhook server with the manager
func AddPolicyWebhook(mgr manager.Manager, opa *opa.Client) error {
	wh := &admission.Webhook{Handler: &validationHandler{opa: opa, client: mgr.GetClient()}}
	mgr.GetWebhookServer().Register("/v1/admit", wh)

	if !*disableCertRotation {
		log.Info("cert rotation is enabled")
		if err := AddRotator(mgr); err != nil {
			return err
		}
	} else {
		log.Info("cert rotation is disabled")
	}
	return nil
}

var _ admission.Handler = &validationHandler{}

type validationHandler struct {
	opa    *opa.Client
	client client.Client

	// for testing
	injectedConfig *v1alpha1.Config
}

// Handle the validation request
func (h *validationHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
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
			vResp.Result.Code = http.StatusInternalServerError
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
		if vResp.Result == nil {
			vResp.Result = &metav1.Status{}
		}
		if userErr {
			vResp.Result.Code = http.StatusUnprocessableEntity
		} else {
			vResp.Result.Code = http.StatusInternalServerError
		}
		return vResp
	}

	resp, err := h.reviewRequest(ctx, req)
	if err != nil {
		log.Error(err, "error executing query")
		vResp := admission.ValidationResponse(false, err.Error())
		if vResp.Result == nil {
			vResp.Result = &metav1.Status{}
		}
		vResp.Result.Code = http.StatusInternalServerError
		return vResp
	}
	res := resp.Results()
	if len(res) != 0 {
		var msgs []string
		for _, r := range res {
			if r.EnforcementAction == "deny" {
				msgs = append(msgs, fmt.Sprintf("[denied by %s] %s", r.Constraint.GetName(), r.Msg))
			}
		}
		if len(msgs) > 0 {
			vResp := admission.ValidationResponse(false, strings.Join(msgs, "\n"))
			if vResp.Result == nil {
				vResp.Result = &metav1.Status{}
			}
			vResp.Result.Code = http.StatusForbidden
			return vResp
		}
	}
	return admission.ValidationResponse(true, "")
}

func (h *validationHandler) getConfig(ctx context.Context) (*v1alpha1.Config, error) {
	if h.injectedConfig != nil {
		return h.injectedConfig, nil
	}
	if h.client == nil {
		return nil, errors.New("no client available to retrieve validation config")
	}
	cfg := &v1alpha1.Config{}
	return cfg, h.client.Get(ctx, config.CfgKey, cfg)
}

func isGkServiceAccount(user authenticationv1.UserInfo) bool {
	saGroup := fmt.Sprintf("system:serviceaccounts:%s", util.GetNamespace())
	for _, g := range user.Groups {
		if g == saGroup {
			return true
		}
	}
	return false
}

// validateGatekeeperResources returns whether an issue is user error (vs internal) and any errors
// validating internal resources
func (h *validationHandler) validateGatekeeperResources(ctx context.Context, req admission.Request) (bool, error) {
	if req.AdmissionRequest.Kind.Group == "templates.gatekeeper.sh" && req.AdmissionRequest.Kind.Kind == "ConstraintTemplate" {
		return h.validateTemplate(ctx, req)
	}
	if req.AdmissionRequest.Kind.Group == "constraints.gatekeeper.sh" {
		return h.validateConstraint(ctx, req)
	}
	return false, nil
}

func (h *validationHandler) validateTemplate(ctx context.Context, req admission.Request) (bool, error) {
	templ, _, err := deserializer.Decode(req.AdmissionRequest.Object.Raw, nil, nil)
	if err != nil {
		return false, err
	}
	unversioned := &templates.ConstraintTemplate{}
	if err := runtimeScheme.Convert(templ, unversioned, nil); err != nil {
		return false, err
	}
	if _, err := h.opa.CreateCRD(ctx, unversioned); err != nil {
		return true, err
	}
	return false, nil
}

func (h *validationHandler) validateConstraint(ctx context.Context, req admission.Request) (bool, error) {
	obj := &unstructured.Unstructured{}
	if _, _, err := deserializer.Decode(req.AdmissionRequest.Object.Raw, nil, obj); err != nil {
		return false, err
	}
	if err := h.opa.ValidateConstraint(ctx, obj); err != nil {
		return true, err
	}

	enforcementActionString, found, err := unstructured.NestedString(obj.Object, "spec", "enforcementAction")
	if err != nil {
		return false, err
	}
	if found && enforcementActionString != "" {
		if *disableEnforcementActionValidation == false {
			err = validateEnforcementAction(enforcementActionString)
			if err != nil {
				return false, err
			}
		}
	} else {
		return true, nil
	}
	return false, nil
}

func validateEnforcementAction(input string) error {
	for _, n := range supportedEnforcementActions {
		if input == n {
			return nil
		}
	}
	return fmt.Errorf("Could not find the provided enforcementAction value within the supported list %v", supportedEnforcementActions)
}

// traceSwitch returns true if a request should be traced
func (h *validationHandler) reviewRequest(ctx context.Context, req admission.Request) (*rtypes.Responses, error) {
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
