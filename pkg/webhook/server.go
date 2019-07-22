package webhook

import (
	"flag"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	namespace        = "gatekeeper-system"
)

var log = logf.Log.WithName("webhook")

var (
	runtimeScheme      = k8sruntime.NewScheme()
	codecs             = serializer.NewCodecFactory(runtimeScheme)
	deserializer       = codecs.UniversalDeserializer() // probably don't need this
	enableManualDeploy = flag.Bool("enable-manual-deploy", false, "allow users to manually create webhook related objects")
	port               = flag.Int("port", 443, "port for the server. defaulted to 443 if unspecified ")
	mutatingWHName     = flag.String("mutating-wh-name", "mutation.gatekeeper.sh", "domain name of the webhook, with at least three segments separated by dots. defaulted to mutation.gatekeeper.sh if unspecified ")
	validatingWHName   = flag.String("validating-wh-name", "validation.gatekeeper.sh", "domain name of the webhook, with at least three segments separated by dots. defaulted to validation.gatekeeper.sh if unspecified ")
)

// InitializeServer creates and registers the server with the manager
func InitializeServer(mgr manager.Manager) (*webhook.Server, error) {
	// A Server registers Webhook Configuration with the apiserver and creates
  // an HTTP server to route requests to the handlers.
	serverOptions := webhook.ServerOptions{
		CertDir: "/certs",
		Port:    int32(*port),
	}

	if *enableManualDeploy == false {
		serverOptions.BootstrapOptions = &webhook.BootstrapOptions{
			ValidatingWebhookConfigName: *validatingWHName,
      MutatingWebhookConfigName: *mutatingWHName,
			Secret: &types.NamespacedName{
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
		return nil, err
	}

  return s, nil
}
