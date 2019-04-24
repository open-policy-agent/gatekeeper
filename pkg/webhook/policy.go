package webhook

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"strings"

	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	apitypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission/builder"
	atypes "sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
)

func init() {
	AddToManagerFuncs = append(AddToManagerFuncs, AddPolicyWebhook)
}

const (
	namespace = "gatekeeper-system"
)

var log = logf.Log.WithName("webhook")

var (
	runtimeScheme      = k8sruntime.NewScheme()
	codecs             = serializer.NewCodecFactory(runtimeScheme)
	deserializer       = codecs.UniversalDeserializer()
	enableManualDeploy = flag.Bool("enable-manual-deploy", false, "allow users to manually create webhook related objects")
	port               = flag.Int("port", 443, "port for the server. defaulted to 443 if unspecified ")
	webhookName        = flag.String("webhook-name", "validation.gatekeeper.sh", "domain name of the webhook, with at least three segments separated by dots. defaulted to validation.gatekeeper.sh if unspecified ")
)

// AddPolicyWebhook registers the policy webhook server with the manager
// below: notations add permissions kube-mgmt needs. Access cannot yet be restricted on a namespace-level granularity
// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
// +kubebuilder:rbac:groups=,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
func AddPolicyWebhook(mgr manager.Manager, opa opa.Client) error {
	validatingWh, err := builder.NewWebhookBuilder().
		Validating().
		Name(*webhookName).
		Path("/v1/admit").
		Rules(admissionregistrationv1beta1.RuleWithOperations{
			Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update},
			Rule: admissionregistrationv1beta1.Rule{
				APIGroups:   []string{"*"},
				APIVersions: []string{"*"},
				Resources:   []string{"*"},
			},
		}).
		Handlers(&validationHandler{opa: opa}).
		WithManager(mgr).
		Build()
	if err != nil {
		return err
	}

	serverOptions := webhook.ServerOptions{
		CertDir: "/certs",
		Port:    int32(*port),
	}

	if *enableManualDeploy == false {
		serverOptions.BootstrapOptions = &webhook.BootstrapOptions{
			ValidatingWebhookConfigName: *webhookName,
			Secret: &apitypes.NamespacedName{
				Namespace: namespace,
				Name:      "gatekeeper-webhook-server-secret",
			},
			Service: &webhook.Service{
				Namespace: namespace,
				Name:      "gatekeeper-controller-manager-service",
				Selectors: map[string]string{
					"control-plane":           "controller-manager",
					"controller-tools.k8s.io": "1.0",
				},
			},
		}
	} else {
		disableWebhookConfigInstaller := true
		serverOptions.DisableWebhookConfigInstaller = &disableWebhookConfigInstaller
	}

	s, err := webhook.NewServer("policy-admission-server", mgr, serverOptions)
	if err != nil {
		return err
	}

	if err := s.Register(validatingWh); err != nil {
		return err
	}

	return nil
}

var _ admission.Handler = &validationHandler{}

type validationHandler struct {
	opa opa.Client
}

func (h *validationHandler) Handle(ctx context.Context, req atypes.Request) atypes.Response {
	log := log.WithValues("hookType", "validation")
	if isGkServiceAccount(req.AdmissionRequest.UserInfo) {
		return admission.ValidationResponse(true, "Gatekeeper does not self-manage")
	}
	resp, err := h.opa.Review(ctx, req.AdmissionRequest)
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
			msgs = append(msgs, r.Msg)
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

func isGkServiceAccount(user authenticationv1.UserInfo) bool {
	saGroup := fmt.Sprintf("system:serviceaccounts:%s", namespace)
	for _, g := range user.Groups {
		if g == saGroup {
			return true
		}
	}
	return false
}
