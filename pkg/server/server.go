package server

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	opa "github.com/Azure/kubernetes-policy-controller/pkg/opa"
	"github.com/Azure/kubernetes-policy-controller/pkg/policies/types"
	mux "github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	runtimeScheme = k8sruntime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

// Server defines the server for the Webhook
type Server struct {
	Handler http.Handler

	router *mux.Router
	addrs  []string
	cert   *tls.Certificate
	Opa    opa.Query
}

// Loop will contain all the calls from the server that we'll be listening on.
type Loop func() error

// New returns a new Server.
func New() *Server {
	s := Server{}
	return &s
}

// Init initializes the server. This function MUST be called before Loop.
func (s *Server) Init(ctx context.Context) (*Server, error) {
	s.initRouter()

	return s, nil
}

// WithAddresses sets the listening addresses that the server will bind to.
func (s *Server) WithAddresses(addrs []string) *Server {
	s.addrs = addrs
	return s
}

// WithCertificate sets the server-side certificate that the server will use.
func (s *Server) WithCertificate(cert *tls.Certificate) *Server {
	s.cert = cert
	return s
}

// WithOPA sets the opa client that the server will use.
func (s *Server) WithOPA(opa opa.Query) *Server {
	s.Opa = opa
	return s
}

// Listeners returns functions that listen and serve connections.
func (s *Server) Listeners() ([]Loop, error) {
	loops := []Loop{}
	for _, addr := range s.addrs {
		parsedURL, err := parseURL(addr, s.cert != nil)
		if err != nil {
			return nil, err
		}
		var loop Loop
		switch parsedURL.Scheme {
		case "http":
			loop, err = s.getListenerForHTTPServer(parsedURL)
		case "https":
			loop, err = s.getListenerForHTTPSServer(parsedURL)
		default:
			err = fmt.Errorf("invalid url scheme %q", parsedURL.Scheme)
		}
		if err != nil {
			return nil, err
		}
		loops = append(loops, loop)
	}

	return loops, nil
}

func (s *Server) getListenerForHTTPServer(u *url.URL) (Loop, error) {
	httpServer := http.Server{
		Addr:    u.Host,
		Handler: s.Handler,
	}

	httpLoop := func() error { return httpServer.ListenAndServe() }

	return httpLoop, nil
}

func (s *Server) getListenerForHTTPSServer(u *url.URL) (Loop, error) {

	if s.cert == nil {
		return nil, fmt.Errorf("TLS certificate required but not supplied")
	}

	httpsServer := http.Server{
		Addr:    u.Host,
		Handler: s.Handler,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{*s.cert},
		},
	}

	httpsLoop := func() error { return httpsServer.ListenAndServeTLS("", "") }

	return httpsLoop, nil
}

func (s *Server) initRouter() {

	router := s.router

	if router == nil {
		router = mux.NewRouter()
	}

	router.UseEncodedPath()
	router.StrictSlash(true)

	s.registerHandler(router, 1, "/validate", http.MethodPost, appHandler(s.Validate))
	s.registerHandler(router, 1, "/mutate", http.MethodPost, appHandler(s.Mutate))
	s.registerHandler(router, 1, "/audit", http.MethodGet, appHandler(s.Audit))

	// default 405
	router.Handle("/mutate/{path:.*}", appHandler(HTTPStatus(405))).Methods(http.MethodHead, http.MethodConnect, http.MethodDelete,
		http.MethodGet, http.MethodOptions, http.MethodTrace, http.MethodPost, http.MethodPut, http.MethodPatch)
	router.Handle("/mutate", appHandler(HTTPStatus(405))).Methods(http.MethodHead,
		http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodOptions, http.MethodTrace, http.MethodPut, http.MethodPatch)
	// default 405
	router.Handle("/validate/{path:.*}", appHandler(HTTPStatus(405))).Methods(http.MethodHead, http.MethodConnect, http.MethodDelete,
		http.MethodGet, http.MethodOptions, http.MethodTrace, http.MethodPost, http.MethodPut, http.MethodPatch)
	router.Handle("/validate", appHandler(HTTPStatus(405))).Methods(http.MethodHead,
		http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodOptions, http.MethodTrace, http.MethodPut, http.MethodPatch)
	// default 405
	router.Handle("/audit/{path:.*}", appHandler(HTTPStatus(405))).Methods(http.MethodHead, http.MethodConnect, http.MethodDelete,
		http.MethodGet, http.MethodOptions, http.MethodTrace, http.MethodPost, http.MethodPut, http.MethodPatch)
	router.Handle("/audit", appHandler(HTTPStatus(405))).Methods(http.MethodHead,
		http.MethodConnect, http.MethodDelete, http.MethodOptions, http.MethodTrace, http.MethodPost, http.MethodPut, http.MethodPatch)

	s.Handler = router
}

// HTTPStatus is used to set a specific status code
// Adapted from https://stackoverflow.com/questions/27711154/what-response-code-to-return-on-a-non-supported-http-method-on-rest
func HTTPStatus(code int) func(logger *log.Entry, w http.ResponseWriter, req *http.Request) {
	return func(logger *log.Entry, w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(code)
	}
}

func (s *Server) registerHandler(router *mux.Router, version int, path string, method string, handler http.Handler) {
	prefix := fmt.Sprintf("/v%d", version)
	router.Handle(prefix+path, handler).Methods(method)
}

// Audit method for reporting current policy complaince of the cluster
func (s *Server) Audit(logger *log.Entry, w http.ResponseWriter, r *http.Request) {
	auditResponse, err := s.audit()
	if err != nil {
		logger.Errorf("error geting audit response: %v", err)
		http.Error(w, fmt.Sprintf("error gettinf audit response: %v", err), http.StatusInternalServerError)
	}
	resp, err := json.Marshal(auditResponse)
	if err != nil {
		logger.Errorf("can not encode response: %v", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
	}
	logger.Infof("ready to write reponse %v...", auditResponse)
	if _, err := w.Write(resp); err != nil {
		logger.Errorf("Can't write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}

// Mutate method for mutation webhook server
func (s *Server) Mutate(logger *log.Entry, w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	if len(body) == 0 {
		logger.Error("empty body")
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}
	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		logger.Errorf("Content-Type=%s, expect application/json", contentType)
		http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
		return
	}
	var admissionResponse *v1beta1.AdmissionResponse
	ar := v1beta1.AdmissionReview{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		logger.Errorf("Can't decode body: %v", err)
		admissionResponse = &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	} else {
		admissionResponse = s.mutate(logger, &ar)
	}
	admissionReview := v1beta1.AdmissionReview{}
	if admissionResponse != nil {
		admissionReview.Response = admissionResponse
		if ar.Request != nil {
			admissionReview.Response.UID = ar.Request.UID
		}
	}
	resp, err := json.Marshal(admissionReview)
	if err != nil {
		logger.Errorf("Can't encode response: %v", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
	}
	logger.Infof("Ready to write reponse %v(%v)...", admissionReview.Response.UID, admissionReview.Response.Allowed)
	if _, err := w.Write(resp); err != nil {
		logger.Errorf("Can't write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}

// Validate method for webhook server
func (s *Server) Validate(logger *log.Entry, w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	if len(body) == 0 {
		logger.Error("empty body")
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}
	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		logger.Errorf("Content-Type=%s, expect application/json", contentType)
		http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
		return
	}
	var admissionResponse *v1beta1.AdmissionResponse
	ar := v1beta1.AdmissionReview{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		logger.Errorf("Can't decode body: %v", err)
		admissionResponse = &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	} else {
		admissionResponse = s.validate(logger, &ar)
	}
	admissionReview := v1beta1.AdmissionReview{}
	if admissionResponse != nil {
		admissionReview.Response = admissionResponse
		if ar.Request != nil {
			admissionReview.Response.UID = ar.Request.UID
		}
	}
	resp, err := json.Marshal(admissionReview)
	if err != nil {
		logger.Errorf("Can't encode response: %v", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
	}
	logger.Infof("Ready to write reponse %v(%v)...", admissionReview.Response.UID, admissionReview.Response.Allowed)
	if _, err := w.Write(resp); err != nil {
		logger.Errorf("Can't write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}

func createPatchFromOPAResult(result []map[string]interface{}) ([]byte, error) {
	var val interface{}
	var ok bool
	if len(result) != 1 {
		return nil, fmt.Errorf("invalid patch opa result, %v", result)
	}
	if val, ok = result[0]["patches"]; !ok {
		return nil, nil
	}
	var patches []interface{}
	if patches, ok = val.([]interface{}); !ok {
		return nil, fmt.Errorf("invalid patch value in opa result %v", val)
	}
	if len(patches) == 0 {
		return nil, nil
	}
	return createPatch(patches)
}

func (s *Server) mutationRequired(req *v1beta1.AdmissionRequest) ([]byte, error) {
	var err error
	var mutationQuery string
	if mutationQuery, err = makeOPAMutationQuery(req); err != nil {
		return nil, err
	}

	result, err := s.Opa.PostQuery(mutationQuery)
	if err != nil && !opa.IsUndefinedErr(err) {
		return nil, fmt.Errorf("opa query failed query=%s err=%v", mutationQuery, err)
	}

	bs, err := createPatchFromOPAResult(result)
	if err != nil {
		panic(err)
	}
	return bs, nil
}

// Check whether the target resoured need to be mutated
func (s *Server) isValid(req *v1beta1.AdmissionRequest) (bool, string, error) {
	var err error
	var validationQuery string
	if validationQuery, err = makeOPAValidationQuery(req); err != nil {
		return false, "", err
	}
	response, err := s.Opa.PostQuery(validationQuery)
	if err != nil && !opa.IsUndefinedErr(err) {
		return false, "", err
	}
	if len(response) == 0 {
		return true, "valid", nil
	}
	bs, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		panic(err)
	}
	return false, string(bs), nil
}

// create mutation patch for resoures
func createPatch(data interface{}) ([]byte, error) {
	bs, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if len(bs) == 0 {
		return nil, nil
	}
	encodedbs := make([]byte, base64.URLEncoding.EncodedLen(len(bs)))
	base64.StdEncoding.Encode(encodedbs, bs)

	return bs, nil
}

func makeOPAWithAsQuery(query, path, value string) string {
	return fmt.Sprintf(`%s with %s as %s`, query, path, value)
}

func makeOPAValidationQuery(req *v1beta1.AdmissionRequest) (string, error) {
	return makeOPAPostQuery(req, "deny")
}

func makeOPAMutationQuery(req *v1beta1.AdmissionRequest) (string, error) {
	return makeOPAPostQuery(req, "patch")
}

func makeOPAPostQuery(req *v1beta1.AdmissionRequest, queryType string) (string, error) {
	var resource, name string
	if resource = strings.ToLower(strings.TrimSpace(req.Resource.Resource)); len(resource) == 0 {
		return resource, fmt.Errorf("resource is empty")
	}
	if name = strings.ToLower(strings.TrimSpace(req.Name)); len(name) == 0 {
		// assign a random name for validation
		name = randStringBytesMaskImprSrc(10)
	}
	var query, path string
	switch resource {
	case "namespaces":
		query = types.MakeSingleClusterResourceQuery(queryType, resource, name)
		path = fmt.Sprintf(`data["kubernetes"]["%s"]["%s"]`, resource, name)
	default:
		var namespace string
		if namespace = strings.ToLower(strings.TrimSpace(req.Namespace)); len(name) == 0 {
			namespace = metav1.NamespaceDefault
		}
		path = fmt.Sprintf(`data["kubernetes"]["%s"]["%s"]["%s"]`, resource, namespace, name)
		query = types.MakeSingleNamespaceResourceQuery(queryType, resource, namespace, name)
	}
	value := string(req.Object.Raw[:])
	return makeOPAWithAsQuery(query, path, value), nil
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

// main validation process
func (s *Server) validate(logger *log.Entry, ar *v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	response := &v1beta1.AdmissionResponse{
		Allowed: true,
	}
	if ar.Request == nil {
		logger.Errorf("AdmissionReview request is nil, +%v", *ar)
		return response
	}
	req := ar.Request
	logger.Infof("AdmissionReview for Resource=%v Kind=%v, Namespace=%v Name=%v UID=%v Operation=%v UserInfo=%v",
		req.Resource, req.Kind, req.Namespace, req.Name, req.UID, req.Operation, req.UserInfo)

	// validate
	valid, reason, err := s.isValid(req)
	if err != nil {
		logger.Errorf("ar=%+v error=%v", ar, err)
		return response
	}
	if valid {
		return response
	}
	return &v1beta1.AdmissionResponse{
		Allowed: false,
		Result: &metav1.Status{
			Message: reason,
		},
	}
}

// main mutation process
func (s *Server) mutate(logger *log.Entry, ar *v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	response := &v1beta1.AdmissionResponse{
		Allowed: true,
	}
	if ar.Request == nil {
		logger.Errorf("AdmissionReview request is nil, +%v", *ar)
		return response
	}
	req := ar.Request
	logger.Infof("AdmissionReview for Resource=%v Kind=%v, Namespace=%v Name=%v UID=%v Operation=%v UserInfo=%v",
		req.Resource, req.Kind, req.Namespace, req.Name, req.UID, req.Operation, req.UserInfo)

	// determine whether to perform mutation
	patchBytes, err := s.mutationRequired(req)
	if err != nil {
		logger.Errorf("ar=%+v error=%v", ar, err)
		return response
	}
	if patchBytes == nil || len(patchBytes) == 0 {
		logger.Infof("No mutation due to policy check, Resource=%v, Namespace=%v Name=%v UID=%v Operation=%v UserInfo=%v", req.Resource.Resource, req.Namespace, req.Name, req.UID, req.Operation, req.UserInfo)
		return &v1beta1.AdmissionResponse{
			Allowed: true,
		}
	}
	logger.Infof("AdmissionResponse: patch=%v\n", string(patchBytes))
	return &v1beta1.AdmissionResponse{
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *v1beta1.PatchType {
			pt := v1beta1.PatchTypeJSONPatch
			return &pt
		}(),
	}
}

// main validation process
func (s *Server) audit() ([]byte, error) {
	validationQuery := types.MakeAuditQuery()
	response, err := s.Opa.PostQuery(validationQuery)
	if err != nil && !opa.IsUndefinedErr(err) {
		return nil, err
	}
	bs, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		panic(err)
	}
	return bs, nil
}

// InstallDefaultAdmissionPolicy will update OPA with a default policy  This function will
// block until the policy has been installed.
func InstallDefaultAdmissionPolicy(id string, policy []byte, opa opa.Policies) error {
	for {
		time.Sleep(time.Second * 1)
		if err := opa.InsertPolicy(id, policy); err != nil {
			log.Errorf("Failed to install default policy (kubernetesPolicy) : %v", err)
		} else {
			return nil
		}
	}
}

type appHandler func(*log.Entry, http.ResponseWriter, *http.Request)

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

// ServeHTTP implements the net/http server handler interface
// and recovers from panics.
func (fn appHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logger := log.WithFields(log.Fields{
		"req.method": r.Method,
		"req.path":   r.URL.Path,
		"req.remote": parseRemoteAddr(r.RemoteAddr),
	})
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
				err = errors.New("Unknown error")
			}
			logger.WithField("res.status", http.StatusInternalServerError).
				Errorf("Panic processing request: %+v, file: %s, line: %d, stacktrace: '%s'", r, file, line, stack)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}()
	rw := newResponseWriter(w)
	fn(logger, rw, r)
	latency := time.Since(start)
	logger.Infof("Status (%d) took %d ns", rw.statusCode, latency.Nanoseconds())
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

func parseURL(s string, useHTTPSByDefault bool) (*url.URL, error) {
	if !strings.Contains(s, "://") {
		scheme := "http://"
		if useHTTPSByDefault {
			scheme = "https://"
		}
		s = scheme + s
	}
	return url.Parse(s)
}
