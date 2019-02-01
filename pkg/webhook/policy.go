package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/mattbaird/jsonpatch"
	"github.com/open-policy-agent/kubernetes-policy-controller/pkg/opa"
	"github.com/open-policy-agent/kubernetes-policy-controller/pkg/policies/types"
	"github.com/open-policy-agent/opa/util"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1beta1 "k8s.io/api/authorization/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	apitypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission/builder"
	atypes "sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
)

func init() {
	AddToManagerFuncs = append(AddToManagerFuncs, AddPolicyWebhook)
}

var (
	runtimeScheme = k8sruntime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

// AddPolicyWebhook registers the policy webhook server with the manager
// below: notations add permissions kube-mgmt needs. Access cannot yet be restricted on a namespace-level granularity
// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
// +kubebuilder:rbac:groups=,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
func AddPolicyWebhook(mgr manager.Manager) error {
	opa := opa.NewFromFlags()

	serverOptions := webhook.ServerOptions{
		CertDir: "/certs",
		BootstrapOptions: &webhook.BootstrapOptions{
			MutatingWebhookConfigName: "kpc",
			Secret: &apitypes.NamespacedName{
				Namespace: "kpc-system",
				Name:      "kpc-webhook-server-secret",
			},
			Service: &webhook.Service{
				Namespace: "kpc-system",
				Name:      "kpc-controller-manager-service",
			},
		},
	}

	s, err := webhook.NewServer("policy-admission-server", mgr, serverOptions)
	if err != nil {
		return err
	}
	return addWebhooks(mgr, s, opa)
}

// GenericHandler is any object that supports the webhook server's Handle() method.
type GenericHandler interface {
	Handle(string, http.Handler)
}

// serverLike is a shim to enable easy testing. This interface supports both endpoint registration
// methods of Kubebuilder's webhook server.
type serverLike interface {
	GenericHandler
	Register(...webhook.Webhook) error
}

// addWebhooks adds all webhooks to the provided server-like object.
func addWebhooks(mgr manager.Manager, server serverLike, opa opa.Query) error {
	AddGenericWebhooks(server, opa)

	mutatingWh, err := builder.NewWebhookBuilder().
		Mutating().
		Name("mutation.styra.com").
		Path("/v1/admit").
		Rules(admissionregistrationv1beta1.RuleWithOperations{
			Operations: []admissionregistrationv1beta1.OperationType{admissionregistrationv1beta1.Create, admissionregistrationv1beta1.Update},
			Rule: admissionregistrationv1beta1.Rule{
				APIGroups:   []string{"*"},
				APIVersions: []string{"*"},
				Resources:   []string{"*"},
			},
		}).
		Handlers(mutationHandler{opa: opa}).
		WithManager(mgr).
		Build()
	if err != nil {
		return err
	}

	if err := server.Register(mutatingWh); err != nil {
		return err
	}

	return nil
}

// AddGenericWebhooks adds all handlers that handle raw HTTP requests.
func AddGenericWebhooks(server GenericHandler, opa opa.Query) {
	auditWh := newGenericWebhook("/v1/audit", &auditHandler{opa: opa}, []string{http.MethodGet})
	authWh := newGenericWebhook("/v1/authorize", &authorizeHandler{opa: opa}, []string{http.MethodPost})

	server.Handle(auditWh.path, auditWh)
	server.Handle(authWh.path, authWh)
}

// Audit method for reporting current policy complaince of the cluster
var _ genericHandler = &auditHandler{}

type auditHandler struct {
	opa opa.Query
}

func (h *auditHandler) Handle(w http.ResponseWriter, r *http.Request) {
	auditResponse, err := h.audit()
	if err != nil {
		glog.Errorf("error geting audit response: %v", err)
		http.Error(w, fmt.Sprintf("error gettinf audit response: %v", err), http.StatusInternalServerError)
	}
	glog.Infof("audit: ready to write reponse %v...", string(auditResponse))
	if _, err := w.Write(auditResponse); err != nil {
		glog.Infof("Can't write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}

// main validation process
func (h *auditHandler) audit() ([]byte, error) {
	validationQuery := types.MakeAuditQuery()
	result, err := h.opa.PostQuery(validationQuery)
	if err != nil && !opa.IsUndefinedErr(err) {
		return nil, err
	}
	bs, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var response types.AuditResponseV1
	err = util.UnmarshalJSON(bs, &response.Violations)
	if err != nil {
		return nil, err
	}
	response.Message = fmt.Sprintf("total violations:%v", len(response.Violations))
	bs, err = json.Marshal(response)
	if err != nil {
		return nil, err
	}

	return bs, nil
}

// Handler for the mutation admission webhook
var _ admission.Handler = mutationHandler{}

type mutationHandler struct {
	opa opa.Query
}

func (h mutationHandler) Handle(ctx context.Context, req atypes.Request) atypes.Response {
	ar := req.AdmissionRequest
	glog.Infof("AdmissionReview for Resource=%v Kind=%v, Namespace=%v Name=%v UID=%v Operation=%v UserInfo=%v", ar.Resource, ar.Kind, ar.Namespace, ar.Name, ar.UID, ar.Operation, ar.UserInfo)

	// do admission policy check
	allowed, reason, patchBytes, err := h.doAdmissionPolicyCheck(ar)

	// Note we are allowing access on an erroring policy check
	if err != nil {
		msg := fmt.Sprintf("policy check failed Resource=%v Kind=%v, Namespace=%v Name=%v UID=%v Operation=%v UserInfo=%v ar=%+v error=%v", ar.Resource, ar.Kind, ar.Namespace, ar.Name, ar.UID, ar.Operation, ar.UserInfo, req, err)
		glog.Infof(msg)
		return admission.ValidationResponse(true, msg)
	}

	if patchBytes == nil || len(patchBytes) == 0 {
		glog.Infof("AdmissionResponse: No mutation due to policy check, Resource=%v, Namespace=%v Name=%v UID=%v Operation=%v UserInfo=%v", ar.Resource.Resource, ar.Namespace, ar.Name, ar.UID, ar.Operation, ar.UserInfo)
		return admission.ValidationResponse(allowed, reason)
	}

	glog.Infof("AdmissionResponse: Mutate Resource=%v Namespace=%v Name=%v UID=%v Operation=%v UserInfo=%v", ar.Resource.Resource, ar.Namespace, ar.Name, ar.UID, ar.Operation, ar.UserInfo)
	patches := []jsonpatch.JsonPatchOperation{}
	if err := json.Unmarshal(patchBytes, &patches); err != nil {
		msg := fmt.Sprintf("poorly formed JSONPatch Resource=%v Kind=%v, Namespace=%v Name=%v UID=%v Operation=%v UserInfo=%v ar=%+v error=%v", ar.Resource, ar.Kind, ar.Namespace, ar.Name, ar.UID, ar.Operation, ar.UserInfo, req, err)
		glog.Infof(msg)
		return admission.ValidationResponse(true, msg)
	}
	return atypes.Response{
		Patches: patches,
		Response: &admissionv1beta1.AdmissionResponse{
			Allowed: true,
			Result: &metav1.Status{
				Message: reason,
			},
			PatchType: func() *admissionv1beta1.PatchType { pt := admissionv1beta1.PatchTypeJSONPatch; return &pt }(),
		},
	}
}

func (h *mutationHandler) doAdmissionPolicyCheck(req *admissionv1beta1.AdmissionRequest) (allowed bool, reason string, patchBytes []byte, err error) {
	var mutationQuery string
	if mutationQuery, err = makeOPAAdmissionPostQuery(req); err != nil {
		return false, "", nil, err
	}

	glog.Infof("Sending admission query to opa: %v", mutationQuery)

	result, err := h.opa.PostQuery(mutationQuery)
	if err != nil && !opa.IsUndefinedErr(err) {
		return false, "", nil, fmt.Errorf("opa query failed query=%s err=%v", mutationQuery, err)
	}

	glog.Infof("Response from admission query to opa: %v", result)

	return createPatchFromOPAResult(result)
}

func createPatchFromOPAResult(result []map[string]interface{}) (allowed bool, reasonStr string, patchBytes []byte, err error) {
	if len(result) == 0 {
		return true, "valid based on configured policies", nil, nil
	}
	var msg string
	bs, err := json.Marshal(result)
	if err != nil {
		return false, msg, nil, err
	}
	var allViolations []types.Deny
	err = util.UnmarshalJSON(bs, &allViolations)
	if err != nil {
		return false, msg, nil, err
	}
	if len(allViolations) == 0 {
		return true, "valid based on configured policies", nil, nil
	}
	valid := true
	var reason struct {
		Reason []string `json:"reason,omitempty"`
	}
	validPatches := map[string]types.PatchOperation{}
	for _, v := range allViolations {
		patchCount := len(v.Resolution.Patches)
		if patchCount == 0 {
			valid = false
			reason.Reason = append(reason.Reason, v.Resolution.Message)
			continue
		}
		for _, p := range v.Resolution.Patches {
			if existing, ok := validPatches[p.Path]; ok {
				msg = fmt.Sprintf("conflicting patches caused denied request, operations (%+v, %+v)", p, existing)
				return false, msg, nil, nil
			}
			validPatches[p.Path] = p
		}
	}
	if !valid {
		if bs, err := json.Marshal(reason.Reason); err == nil {
			msg = string(bs)
		}
		return false, msg, nil, nil
	}
	var patches []interface{}
	for _, p := range validPatches {
		patches = append(patches, p)
	}
	if len(patches) == 0 {
		panic(fmt.Errorf("unexpected no valid patches found, %+v", allViolations))
	}
	patchBytes, err = json.Marshal(patches)
	if err != nil {
		return false, "", nil, fmt.Errorf("failed creating patches, patches=%+v err=%v", patches, err)
	}

	return true, "applying patches", patchBytes, nil
}

func makeOPAWithAsQuery(query, path, value string) string {
	return fmt.Sprintf(`%s with %s as %s`, query, path, value)
}

func makeOPAAdmissionPostQuery(req *admissionv1beta1.AdmissionRequest) (string, error) {
	var resource, name string
	if resource = strings.ToLower(strings.TrimSpace(req.Resource.Resource)); len(resource) == 0 {
		return resource, fmt.Errorf("resource is empty")
	}
	if name = strings.ToLower(strings.TrimSpace(req.Name)); len(name) == 0 {
		// assign a random name for validation
		name = randStringBytesMaskImprSrc(10)
	}
	// TODO: I think we have an Issue here, what happens to other cluster-wide resources except namespaces?
	// Right now they get also the default Namespace in the default clause
	var query, path string
	switch resource {
	case "namespaces":
		query = types.MakeSingleClusterResourceQuery(resource, name)
		path = fmt.Sprintf(`data["kubernetes"]["%s"]["%s"]`, resource, name)
	default:
		var namespace string
		if namespace = strings.ToLower(strings.TrimSpace(req.Namespace)); len(namespace) == 0 {
			namespace = metav1.NamespaceDefault
		}
		path = fmt.Sprintf(`data["kubernetes"]["%s"]["%s"]["%s"]`, resource, namespace, name)
		query = types.MakeSingleNamespaceResourceQuery(resource, namespace, name)
	}

	value, err := createAdmissionRequestValueForOPA(req)
	if err != nil {
		return "", err
	}
	return makeOPAWithAsQuery(query, path, value), nil
}

type admissionRequest struct {
	UID         string                      `json:"uid" protobuf:"bytes,1,opt,name=uid"`
	Kind        metav1.GroupVersionKind     `json:"kind" protobuf:"bytes,2,opt,name=kind"`
	Resource    metav1.GroupVersionResource `json:"resource" protobuf:"bytes,3,opt,name=resource"`
	SubResource string                      `json:"subResource,omitempty" protobuf:"bytes,4,opt,name=subResource"`
	Name        string                      `json:"name,omitempty" protobuf:"bytes,5,opt,name=name"`
	Namespace   string                      `json:"namespace,omitempty" protobuf:"bytes,6,opt,name=namespace"`
	Operation   string                      `json:"operation" protobuf:"bytes,7,opt,name=operation"`
	UserInfo    authenticationv1.UserInfo   `json:"userInfo" protobuf:"bytes,8,opt,name=userInfo"`
	Object      json.RawMessage             `json:"object,omitempty" protobuf:"bytes,9,opt,name=object"`
	OldObject   json.RawMessage             `json:"oldObject,omitempty" protobuf:"bytes,10,opt,name=oldObject"`
}

func createAdmissionRequestValueForOPA(req *admissionv1beta1.AdmissionRequest) (string, error) {
	ar := admissionRequest{
		UID:         string(req.UID),
		Kind:        req.Kind,
		Resource:    req.Resource,
		SubResource: req.SubResource,
		Name:        req.Name,
		Namespace:   req.Namespace,
		Operation:   string(req.Operation),
		UserInfo:    req.UserInfo,
		Object:      req.Object.Raw[:],
		OldObject:   req.OldObject.Raw[:],
	}
	reqJson, err := json.Marshal(ar)
	if err != nil {
		return "", fmt.Errorf("error marshalling AdmissionRequest: %v", err)
	}
	return string(reqJson), nil
}

// Authorize method for authorization module webhook server
var _ genericHandler = &authorizeHandler{}

type authorizeHandler struct {
	opa opa.Query
}

func (h *authorizeHandler) Handle(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	if len(body) == 0 {
		glog.Error("empty body")
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}
	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		glog.Errorf("Content-Type=%s, expect application/json", contentType)
		http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
		return
	}
	sar := authorizationv1beta1.SubjectAccessReview{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &sar); err != nil {
		glog.Errorf("Can't decode body %v: %v", string(body), err)
		sar = authorizationv1beta1.SubjectAccessReview{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "authorization.k8s.io/v1beta1",
				Kind:       "SubjectAccessReview",
			},
			Status: authorizationv1beta1.SubjectAccessReviewStatus{
				Allowed:         false,
				Denied:          true,
				EvaluationError: err.Error(),
			},
		}
	} else {
		sar.Status = h.authorizationPolicyCheck(&sar)
	}
	resp, err := json.Marshal(sar)
	if err != nil {
		glog.Errorf("Can't encode response: %v", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
	}
	glog.Infof("SubjectAccessResponse: (denied: %v)\n", sar.Status.Denied)
	if _, err := w.Write(resp); err != nil {
		glog.Errorf("Can't write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}

// main authorization process
func (h *authorizeHandler) authorizationPolicyCheck(sar *authorizationv1beta1.SubjectAccessReview) authorizationv1beta1.SubjectAccessReviewStatus {
	response := authorizationv1beta1.SubjectAccessReviewStatus{
		Allowed: false,
		Denied:  true,
	}
	attributes := sarAttributes(sar)
	glog.Infof("SubjectAccessReview for %s", attributes)

	// do authorization policy check
	allowed, reason, err := h.doAuthorizationPolicyCheck(sar)
	if err != nil {
		glog.Infof("policy check failed %s ar=%+v error=%v", attributes, sar, err)
		return response
	}
	glog.Infof("SubjectAccessReviewStatus: denied: %t reason: %s %s", !allowed, reason, attributes)
	return authorizationv1beta1.SubjectAccessReviewStatus{
		Allowed: false,
		Denied:  !allowed,
		Reason:  reason,
	}
}

func sarAttributes(sar *authorizationv1beta1.SubjectAccessReview) string {
	var attrs []string
	if sar.Spec.ResourceAttributes != nil {
		attrs = append(attrs, fmt.Sprintf("Group=%v Version=%v Resource=%v Subresource=%v Namespace=%v Name=%v Verb=%v", sar.Spec.ResourceAttributes.Group, sar.Spec.ResourceAttributes.Version, sar.Spec.ResourceAttributes.Resource, sar.Spec.ResourceAttributes.Subresource, sar.Spec.ResourceAttributes.Namespace, sar.Spec.ResourceAttributes.Name, sar.Spec.ResourceAttributes.Verb))
	}
	attrs = append(attrs, fmt.Sprintf("User=%v Groups=%v", sar.Spec.User, sar.Spec.Groups))
	return strings.Join(attrs, " ")
}

func (h *authorizeHandler) doAuthorizationPolicyCheck(sar *authorizationv1beta1.SubjectAccessReview) (allowed bool, reason string, err error) {
	var authorizationQuery string
	if authorizationQuery, err = makeOPAAuthorizationPostQuery(sar); err != nil {
		return false, "", err
	}

	glog.Infof("Sending authorization query to opa: %v", authorizationQuery)

	result, err := h.opa.PostQuery(authorizationQuery)

	if err != nil && !opa.IsUndefinedErr(err) {
		return false, fmt.Sprintf("opa query failed query=%s err=%v", authorizationQuery, err), err
	}

	glog.Infof("Response from authorization query to opa: %v", result)

	return parseOPAResult(result)
}

func parseOPAResult(result []map[string]interface{}) (allowed bool, reasonStr string, err error) {
	if len(result) == 0 {
		return true, "", nil
	}
	var msg string
	bs, err := json.Marshal(result)
	if err != nil {
		return false, msg, err
	}
	var allViolations []types.Deny
	err = util.UnmarshalJSON(bs, &allViolations)
	if err != nil {
		return false, msg, err
	}
	if len(allViolations) == 0 {
		return true, "", nil
	}
	var reason struct {
		Reason []string `json:"reason,omitempty"`
	}
	for _, v := range allViolations {
		reason.Reason = append(reason.Reason, v.Resolution.Message)
	}
	if bs, err := json.Marshal(reason.Reason); err == nil {
		msg = string(bs)
	}
	return false, msg, nil
}

func makeOPAAuthorizationPostQuery(sar *authorizationv1beta1.SubjectAccessReview) (string, error) {

	var query, path string
	// resource requests
	if sar.Spec.ResourceAttributes != nil {

		var resource, name string
		if resource = strings.ToLower(strings.TrimSpace(sar.Spec.ResourceAttributes.Resource)); len(resource) == 0 {
			return resource, fmt.Errorf("resource is empty")
		}
		if name = strings.ToLower(strings.TrimSpace(sar.Spec.ResourceAttributes.Name)); len(name) == 0 {
			// assign a random name for validation
			name = randStringBytesMaskImprSrc(10)
		}

		// We could determine single namespace or cluster query based on if the namespace is set in the SubjectAccessReview, e.g.:
		// 	List across namespaces +> the namespace field will be empty, e.g.: kubectl get pods --all-namespaces
		//  List pods in a single namespace +> the namespace field will be set, e.g.: kubectl get pods -n my-namespace
		//  Get a specific pod in a namespace +> the namespace field will be set, e.g.: kubectl get pod/mypod -n my-namespace`
		//  Get a cluster-scoped resource +> namespace will be empty, e.g.: kubectl get clusterroles

		// The problem is because we don't want to write separate rules for, e.g. the pod case (1 for all-namespaces & 1 for
		// -n my-namespace) we just always send a namespace query, if we have no namespace from the request, we just set it empty.
		// Namespaced Resource
		query = types.MakeSingleNamespaceAuthorizationResourceQuery(resource, sar.Spec.ResourceAttributes.Namespace, name)
		path = fmt.Sprintf(`data["kubernetes"]["%s"]["%s"]["%s"]`, resource, sar.Spec.ResourceAttributes.Namespace, name)
	} else {
		// non-resource requests
		if sar.Spec.NonResourceAttributes != nil {
			// None is used for now to identify the kind of non-resource requests
			query = types.MakeSingleNamespaceAuthorizationResourceQuery("None", "", sar.Spec.NonResourceAttributes.Path)
			path = fmt.Sprintf(`data["kubernetes"]["%s"]["%s"]`, "None", sar.Spec.NonResourceAttributes.Path)
		} else {
			return "", fmt.Errorf("unknown request type, resource is neither resource nor non-resource request")
		}
	}

	sarJson, err := json.Marshal(sar)
	if err != nil {
		return "", fmt.Errorf("error marshalling SubjectAccessReview: %v", err)
	}
	return makeOPAWithAsQuery(query, path, string(sarJson)), nil
}

var src = rand.NewSource(time.Now().UnixNano())

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

func randStringBytesMaskImprSrc(n int) string {
	b := make([]byte, n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return string(b)
}

// Generic HTTP methods and structs
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{w, http.StatusOK}
}

var _ http.Handler = GenericWebhook{}

type genericHandler interface {
	Handle(http.ResponseWriter, *http.Request)
}

type GenericWebhook struct {
	handler genericHandler
	path    string
	methods []string
}

func newGenericWebhook(path string, handler genericHandler, methods []string) GenericWebhook {
	return GenericWebhook{
		path:    path,
		methods: methods,
		handler: handler,
	}
}

// ServeHTTP implements the net/http server handler interface
// and recovers from panics.
func (gw GenericWebhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Received request: method=%v, path=%v, remote=%v", r.Method, r.URL.Path, parseRemoteAddr(r.RemoteAddr))
	if !gw.allowedMethod(r.Method) || !gw.allowedPath(r.URL.Path) {
		w.WriteHeader(405)
		return
	}

	start := time.Now()
	defer func() {
		var err error
		if rec := recover(); rec != nil {
			_, file, line, _ := runtime.Caller(3)
			stack := string(debug.Stack())
			switch t := rec.(type) {
			case string:
				err = errors.New(t)
			case error:
				err = t
			default:
				err = errors.New("unknown error")
			}
			glog.Errorf("Panic processing request: %+v, file: %s, line: %d, stacktrace: '%s'", r, file, line, stack)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}()
	rw := newResponseWriter(w)
	gw.handler.Handle(rw, r)
	latency := time.Since(start)
	glog.Infof("Status (%d) took %d ns", rw.statusCode, latency.Nanoseconds())
}

func (gw GenericWebhook) allowedMethod(method string) bool {
	for _, m := range gw.methods {
		if m == method {
			return true
		}
	}
	return false
}

func (gw GenericWebhook) allowedPath(path string) bool {
	extra := strings.Replace(path, gw.path, "", 1)
	if extra != "" && extra != "/" {
		return false
	}
	return true
}

func parseRemoteAddr(addr string) string {
	n := strings.IndexByte(addr, ':')
	if n <= 1 {
		return ""
	}
	hostname := addr[0:n]
	if net.ParseIP(hostname) == nil {
		return ""
	}
	return hostname
}
