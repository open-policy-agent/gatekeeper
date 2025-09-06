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

package webhook

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/open-policy-agent/cert-controller/pkg/rotator"
	"github.com/open-policy-agent/gatekeeper/v3/apis"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	mutationtypes "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func init() {
	AddToManagerFuncs = append(AddToManagerFuncs, AddMutatingWebhook)

	if err := apis.AddToScheme(runtimeScheme); err != nil {
		log.Error(err, "unable to add to scheme")
		panic(err)
	}
}

// +kubebuilder:webhook:verbs=create;update,path=/v1/mutate,mutating=true,failurePolicy=ignore,groups=*,resources=*,versions=*,name=mutation.gatekeeper.sh,sideEffects=None,admissionReviewVersions=v1;v1beta1,matchPolicy=Exact
// +kubebuilder:rbac:resourceNames=gatekeeper-mutating-webhook-configuration,groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;list;watch;update;patch

// AddMutatingWebhook registers the mutating webhook server with the manager.
func AddMutatingWebhook(mgr manager.Manager, deps Dependencies) error {
	if !operations.IsAssigned(operations.MutationWebhook) {
		return nil
	}
	reporter, err := newStatsReporter()
	if err != nil {
		return err
	}
	eventBroadcaster := record.NewBroadcaster()
	kubeClient := kubernetes.NewForConfigOrDie(mgr.GetConfig())

	log := log.WithValues("hookType", "mutation")
	eventBroadcaster.StartRecordingToSink(&clientcorev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(
		scheme.Scheme,
		corev1.EventSource{Component: "gatekeeper-mutation-webhook"})

	wh := &admission.Webhook{
		Handler: &mutationHandler{
			webhookHandler: webhookHandler{
				client:          mgr.GetClient(),
				reader:          mgr.GetAPIReader(),
				reporter:        reporter,
				processExcluder: deps.ProcessExcluder,
				eventRecorder:   recorder,
				gkNamespace:     util.GetNamespace(),
			},
			mutationSystem: deps.MutationSystem,
			deserializer:   codecs.UniversalDeserializer(),
			log:            log,
		},
	}

	mgr.GetWebhookServer().Register("/v1/mutate", wh)

	return nil
}

var _ admission.Handler = &mutationHandler{}

type mutationHandler struct {
	webhookHandler
	mutationSystem *mutation.System
	deserializer   runtime.Decoder
	log            logr.Logger
}

// Handle the mutation request
// nolint: gocritic // Must accept admission.Request to satisfy interface.
func (h *mutationHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	timeStart := time.Now()

	if isGkServiceAccount(req.UserInfo) {
		return admission.Allowed("Gatekeeper does not self-manage")
	}

	if req.Operation != admissionv1.Create &&
		req.Operation != admissionv1.Update {
		return admission.Allowed("Mutating only on create or update")
	}

	if h.isGatekeeperResource(&req) {
		return admission.Allowed("Not mutating gatekeeper resources")
	}

	requestResponse := unknownResponse
	defer func() {
		if h.reporter != nil {
			if err := h.reporter.ReportMutationRequest(ctx, requestResponse, time.Since(timeStart)); err != nil {
				h.log.Error(err, "failed to report request")
			}
		}
	}()

	// namespace is excluded from webhook using config
	isExcludedNamespace, err := h.skipExcludedNamespace(&req.AdmissionRequest, process.Mutation)
	if err != nil {
		h.log.Error(err, "error while excluding namespace")
	}

	if isExcludedNamespace {
		requestResponse = skipResponse
		return admission.Allowed("Namespace is set to be ignored by Gatekeeper config")
	}

	resp := h.mutateRequest(ctx, &req)
	requestResponse = successResponse
	return resp
}

func (h *mutationHandler) mutateRequest(ctx context.Context, req *admission.Request) admission.Response {
	ns := &corev1.Namespace{}

	// if the object being mutated is a namespace itself, we use it as namespace
	switch {
	case req.Kind.Kind == namespaceKind && req.Kind.Group == "":
		req.Namespace = ""
		obj, _, err := deserializer.Decode(req.Object.Raw, nil, &corev1.Namespace{})
		if err != nil {
			return admission.Errored(int32(http.StatusInternalServerError), err)
		}
		ok := false
		ns, ok = obj.(*corev1.Namespace)
		if !ok {
			return admission.Errored(int32(http.StatusInternalServerError), errors.New("failed to cast namespace object"))
		}
	case req.Namespace != "":
		if err := h.client.Get(ctx, types.NamespacedName{Name: req.Namespace}, ns); err != nil {
			if !k8serrors.IsNotFound(err) {
				h.log.Error(err, "error retrieving namespace", "name", req.Namespace)
				return admission.Errored(int32(http.StatusInternalServerError), err)
			}
			// bypass cached client and ask api-server directly
			err = h.reader.Get(ctx, types.NamespacedName{Name: req.Namespace}, ns)
			if err != nil {
				h.log.Error(err, "error retrieving namespace from API server", "name", req.Namespace)
				return admission.Errored(int32(http.StatusInternalServerError), err)
			}
		}
	default:
		ns = nil
	}
	obj := unstructured.Unstructured{}
	err := obj.UnmarshalJSON(req.Object.Raw)
	if err != nil {
		h.log.Error(err, "failed to unmarshal", "object", string(req.Object.Raw))
		return admission.Errored(int32(http.StatusInternalServerError), err)
	}

	// It is possible for the namespace to not be populated on an object.
	// Assign the namespace from the request object (which will have the appropriate
	// value), then restore the original value at the end to avoid sending a namespace patch.
	oldNS := obj.GetNamespace()
	obj.SetNamespace(req.Namespace)

	mutable := &mutationtypes.Mutable{
		Object:    &obj,
		Namespace: ns,
		Username:  req.UserInfo.Username,
		Source:    mutationtypes.SourceTypeOriginal,
	}
	mutated, err := h.mutationSystem.Mutate(mutable)
	if err != nil {
		h.log.Error(err, "failed to mutate object", "object", string(req.Object.Raw))
		return admission.Errored(int32(http.StatusInternalServerError), err)
	}
	if !mutated {
		return admission.Allowed("Resource was not mutated")
	}

	mutable.Object.SetNamespace(oldNS)

	newJSON, err := mutable.Object.MarshalJSON()
	if err != nil {
		h.log.Error(err, "failed to marshal mutated object", "object", obj)
		return admission.Errored(int32(http.StatusInternalServerError), err)
	}
	resp := admission.PatchResponseFromRaw(req.Object.Raw, newJSON)
	return resp
}

func AppendMutationWebhookIfEnabled(webhooks []rotator.WebhookInfo) []rotator.WebhookInfo {
	if operations.IsAssigned(operations.MutationWebhook) {
		webhooks = append(webhooks, rotator.WebhookInfo{
			Name: *MwhName,
			Type: rotator.Mutating,
		})
		for _, addlMwh := range strings.Split(*AdditionalMwhNamesToRotateCerts, ",") {
			if addlMwh != *MwhName && addlMwh != "" {
				webhooks = append(webhooks, rotator.WebhookInfo{
					Name: addlMwh,
					Type: rotator.Mutating,
				})
			}
		}
	}
	return webhooks
}
