package webhook

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/open-policy-agent/gatekeeper/v3/apis"
	"github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/keys"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type requestResponse string

const (
	successResponse requestResponse = "success"
	errorResponse   requestResponse = "error"
	denyResponse    requestResponse = "deny"
	allowResponse   requestResponse = "allow"
	unknownResponse requestResponse = "unknown"
	skipResponse    requestResponse = "skip"
)

var log = logf.Log.WithName("webhook")

const (
	serviceAccountName = "gatekeeper-admin"
	mutationsGroup     = "mutations.gatekeeper.sh"
	externalDataGroup  = "externaldata.gatekeeper.sh"
	namespaceKind      = "Namespace"
)

var (
	runtimeScheme                      = k8sruntime.NewScheme()
	codecs                             = serializer.NewCodecFactory(runtimeScheme)
	deserializer                       = codecs.UniversalDeserializer()
	disableEnforcementActionValidation = flag.Bool("disable-enforcementaction-validation", false, "disable validation of the enforcementAction and scopedEnforcementActions field of a constraint")
	logDenies                          = flag.Bool("log-denies", false, "log detailed info on each deny")
	emitAdmissionEvents                = flag.Bool("emit-admission-events", false, "(alpha) emit Kubernetes events for each admission violation")
	admissionEventsInvolvedNamespace   = flag.Bool("admission-events-involved-namespace", false, "emit admission events for each violation in the involved objects namespace, the default (false) generates events in the namespace Gatekeeper is installed in. Admission events from cluster-scoped resources will still follow the default behavior")
	logStatsAdmission                  = flag.Bool("log-stats-admission", false, "(alpha) log stats for admission webhook")
	serviceaccount                     = fmt.Sprintf("system:serviceaccount:%s:%s", util.GetNamespace(), serviceAccountName)
	VwhName                            = flag.String("validating-webhook-configuration-name", "gatekeeper-validating-webhook-configuration", "name of the ValidatingWebhookConfiguration")
	MwhName                            = flag.String("mutating-webhook-configuration-name", "gatekeeper-mutating-webhook-configuration", "name of the MutatingWebhookConfiguration")
	TLSMinVersion                      = flag.String("tls-min-version", "1.3", "minimum version of TLS supported")
	ClientCAName                       = flag.String("client-ca-name", "", "name of the certificate authority bundle to authenticate the Kubernetes API server requests against")
	CertCNName                         = flag.String("client-cn-name", "kube-apiserver", "expected CN name on the client certificate attached by apiserver in requests to the webhook")
)

func ParseTLSVersion(v string) (uint16, error) {
	switch v {
	case "":
		return tls.VersionTLS10, nil
	case "1.0":
		return tls.VersionTLS10, nil
	case "1.1":
		return tls.VersionTLS11, nil
	case "1.2":
		return tls.VersionTLS12, nil
	case "1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, errors.New("invalid TLS version. Must be one of: 1.0, 1.1, 1.2, or 1.3")
	}
}

func init() {
	_ = apis.AddToScheme(runtimeScheme)
}

func isGkServiceAccount(user authenticationv1.UserInfo) bool {
	return user.Username == serviceaccount
}

type webhookHandler struct {
	client   client.Client
	reporter StatsReporter
	// reader that will be configured to use the API server
	// obtained from mgr.GetAPIReader()
	reader client.Reader
	// for testing
	injectedConfig  *v1alpha1.Config
	processExcluder *process.Excluder
	eventRecorder   record.EventRecorder
	gkNamespace     string
}

func (h *webhookHandler) getConfig(ctx context.Context) (*v1alpha1.Config, error) {
	if h.injectedConfig != nil {
		return h.injectedConfig, nil
	}
	if h.client == nil {
		return nil, errors.New("no client available to retrieve validation config")
	}
	cfg := &v1alpha1.Config{}
	return cfg, h.client.Get(ctx, keys.Config, cfg)
}

// isGatekeeperResource returns true if the request relates to a gatekeeper resource.
func (h *webhookHandler) isGatekeeperResource(req *admission.Request) bool {
	if req.AdmissionRequest.Kind.Group == "templates.gatekeeper.sh" ||
		req.AdmissionRequest.Kind.Group == "constraints.gatekeeper.sh" ||
		req.AdmissionRequest.Kind.Group == mutationsGroup ||
		req.AdmissionRequest.Kind.Group == "config.gatekeeper.sh" ||
		req.AdmissionRequest.Kind.Group == externalDataGroup ||
		req.AdmissionRequest.Kind.Group == "expansion.gatekeeper.sh" ||
		req.AdmissionRequest.Kind.Group == "status.gatekeeper.sh" {
		return true
	}

	return false
}

func (h *webhookHandler) tracingLevel(ctx context.Context, req *admission.Request) (bool, bool) {
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
			if strings.EqualFold(trace.Dump, "All") {
				dump = true
			}
		}
	}
	return traceEnabled, dump
}

func (h *webhookHandler) skipExcludedNamespace(req *admissionv1.AdmissionRequest, excludedProcess process.Process) (bool, error) {
	obj := &unstructured.Unstructured{}
	if _, _, err := deserializer.Decode(req.Object.Raw, nil, obj); err != nil {
		return false, err
	}
	obj.SetNamespace(req.Namespace)

	isNamespaceExcluded, err := h.processExcluder.IsNamespaceExcluded(excludedProcess, obj)
	if err != nil {
		return false, err
	}

	return isNamespaceExcluded, err
}

func GetCertNameVerifier() func(cs tls.ConnectionState) error {
	return func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) > 0 {
			if cs.PeerCertificates[0].Subject.CommonName != *CertCNName {
				return fmt.Errorf("x509: subject with cn=%s do not identify as %s", cs.PeerCertificates[0].Subject.CommonName, *CertCNName)
			}
			return nil
		}
		return fmt.Errorf("failed to verify CN name of certificate")
	}
}
