package webhook

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/open-policy-agent/gatekeeper/pkg/opa"
	"github.com/open-policy-agent/gatekeeper/pkg/policies/types"
	opatypes "github.com/open-policy-agent/opa/server/types"
	"github.com/open-policy-agent/opa/util"
	"k8s.io/api/admission/v1beta1"
	authorizationv1beta1 "k8s.io/api/authorization/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func TestAuditWithValidateViolation(t *testing.T) {
	f := newFixture()

	fakeOpa := &opa.FakeOPA{}
	fakeOpa.SetViolation(``, opa.MakeDenyObject("anyID", "anyKind", "anyName", "anyNamespace", "anyMessage", nil))
	mgr, _ := manager.New(&rest.Config{}, manager.Options{})
	addWebhooks(mgr, f.server, fakeOpa)

	setup := []tr{
		{"violation", http.MethodGet, "/audit", "", 200, `{"message": "total violations:1","violations": [{"id": "anyID","resolution": {"message": "anyMessage"},"resource": {"kind": "anyKind","name": "anyName","namespace": "anyNamespace"}}]}`},
	}

	for _, tr := range setup {
		t.Run(tr.name, func(t *testing.T) {
			req := newReqV1(tr.method, tr.path, tr.body)
			req.RemoteAddr = "testaddr"

			if err := f.executeRequest(req, tr.code, tr.resp); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSingleValidation(t *testing.T) {
	f := newFixture()

	fakeOpa := &opa.FakeOPA{}
	fakeOpa.SetViolation(`anyname.*`, opa.MakeDenyObject("anyID", "anyKind", "anyName", "anyNamespace", "anyMessage", nil))
	mgr, _ := manager.New(&rest.Config{}, manager.Options{})
	addWebhooks(mgr, f.server, fakeOpa)

	violationRequest := makeAdmissionRequest("anyKind", "anyName", "anyNamespace")
	validRequest := makeAdmissionRequest("anyKind", "validName", "validNamespace")

	setup := []tr{
		{"violation", http.MethodPost, "/admit", violationRequest, 200, `{"response": {"allowed": false,"status": {"reason": "[\"anyMessage\"]","metadata": {},"code": 200},"uid": "anyUID"}}`},
		// "W10=" is the base64 encoding of "[]"
		{"valid", http.MethodPost, "/admit", validRequest, 200, `{"response": {"allowed": true, "patch": "W10=", "patchType": "JSONPatch",  "status": {"metadata": {},"code": 200}, "uid": "anyUID"}}`},
	}

	for _, tr := range setup {
		t.Run(tr.name, func(t *testing.T) {
			req := newReqV1(tr.method, tr.path, tr.body)
			req.RemoteAddr = "testaddr"

			if err := f.executeRequest(req, tr.code, tr.resp); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMultipleValidation(t *testing.T) {
	f := newFixture()

	fakeOpa := &opa.FakeOPA{}
	fakeOpa.SetViolation(`anyname.*`, opa.MakeDenyObject("anyID", "anyKind", "anyName", "anyNamespace", "anyMessage1", nil),
		opa.MakeDenyObject("anyID", "anyKind", "anyName", "anyNamespace", "anyMessage2", nil))
	mgr, _ := manager.New(&rest.Config{}, manager.Options{})
	addWebhooks(mgr, f.server, fakeOpa)

	violationRequest := makeAdmissionRequest("anyKind", "anyName", "anyNamespace")

	setup := []tr{
		{"violation", http.MethodPost, "/admit", violationRequest, 200, `{"response": {"allowed": false,"status": {"reason": "[\"anyMessage1\",\"anyMessage2\"]","metadata": {},"code": 200},"uid": "anyUID"}}`},
	}

	for _, tr := range setup {
		t.Run(tr.name, func(t *testing.T) {
			req := newReqV1(tr.method, tr.path, tr.body)
			req.RemoteAddr = "testaddr"

			if err := f.executeRequest(req, tr.code, tr.resp); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSingleMutation(t *testing.T) {
	f := newFixture()

	fakeOpa := &opa.FakeOPA{}
	patches := []types.PatchOperation{{Op: "anyOp", Path: "anyPath", Value: "anyValue"}}
	fakeOpa.SetViolation(`anyname.*`, opa.MakeDenyObject("anyID", "anyKind", "anyName", "anyNamespace", "anyMessage", patches))
	mgr, _ := manager.New(&rest.Config{}, manager.Options{})
	addWebhooks(mgr, f.server, fakeOpa)

	expectedPatchBytes, err := getPatchBytes(patches)
	if err != nil {
		t.Fatal(err)
	}
	expectedRespBody := fmt.Sprintf(`{"response": {"allowed": true, "patch": "W10=", "patchType": "JSONPatch",  "patch": "%s", "patchType": "JSONPatch",  "status": {"metadata": {},"code": 200}, "uid": "anyUID"}}`, string(expectedPatchBytes))

	mutationRequest := makeAdmissionRequest("anyKind", "anyName", "anyNamespace")
	setup := []tr{
		{"mutation", http.MethodPost, "/admit", mutationRequest, 200, expectedRespBody},
	}

	for _, tr := range setup {
		t.Run(tr.name, func(t *testing.T) {
			req := newReqV1(tr.method, tr.path, tr.body)
			req.RemoteAddr = "testaddr"

			if err := f.executeRequest(req, tr.code, tr.resp); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMultipleNonConflictingMutation(t *testing.T) {
	f := newFixture()
	fakeOpa := &opa.FakeOPA{}
	// TODO: Flaky, order of patches not guaranteed
	patches := []types.PatchOperation{{Op: "anyOp", Path: "anyPath1", Value: "anyValue"}, {Op: "anyOp", Path: "anyPath2", Value: "anyValue"}}
	fakeOpa.SetViolation(`anyname.*`, opa.MakeDenyObject("anyID", "anyKind", "anyName", "anyNamespace", "anyMessage", patches))
	mgr, _ := manager.New(&rest.Config{}, manager.Options{})
	addWebhooks(mgr, f.server, fakeOpa)
	expectedPatchBytes, err := getPatchBytes(patches)
	if err != nil {
		t.Fatal(err)
	}
	expectedRespBody := fmt.Sprintf(`{"response": {"allowed": true, "patch": "W10=", "patchType": "JSONPatch",  "patch": "%s", "patchType": "JSONPatch",  "status": {"metadata": {},"code": 200}, "uid": "anyUID"}}`, string(expectedPatchBytes))
	mutationRequest := makeAdmissionRequest("anyKind", "anyName", "anyNamespace")
	setup := []tr{
		{"mutation", http.MethodPost, "/admit", mutationRequest, 200, expectedRespBody},
	}
	for _, tr := range setup {
		t.Run(tr.name, func(t *testing.T) {
			req := newReqV1(tr.method, tr.path, tr.body)
			req.RemoteAddr = "testaddr"

			if err := f.executeRequest(req, tr.code, tr.resp); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMultipleConflictingMutation(t *testing.T) {
	f := newFixture()
	fakeOpa := &opa.FakeOPA{}
	patches := []types.PatchOperation{{Op: "anyOp", Path: "anyPath", Value: "anyValue"}, {Op: "anyOp", Path: "anyPath", Value: "anyValue"}}
	fakeOpa.SetViolation(`anyname.*`, opa.MakeDenyObject("anyID", "anyKind", "anyName", "anyNamespace", "anyMessage", patches))
	mgr, _ := manager.New(&rest.Config{}, manager.Options{})
	addWebhooks(mgr, f.server, fakeOpa)
	mutationRequest := makeAdmissionRequest("anyKind", "anyName", "anyNamespace")
	setup := []tr{
		{"mutation", http.MethodPost, "/admit", mutationRequest, 200, `{"response": {"allowed": false, "status": {"reason": "conflicting patches caused denied request, operations ({Op:anyOp Path:anyPath Value:anyValue}, {Op:anyOp Path:anyPath Value:anyValue})","metadata": {},"code": 200},"uid": "anyUID"}}`},
	}
	for _, tr := range setup {
		t.Run(tr.name, func(t *testing.T) {
			req := newReqV1(tr.method, tr.path, tr.body)
			req.RemoteAddr = "testaddr"

			if err := f.executeRequest(req, tr.code, tr.resp); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPatchResultBasic(t *testing.T) {
	var expected opatypes.QueryResponseV1
	err := util.UnmarshalJSON([]byte(`{"result":[{"id":"conditional-annotation","resolution":{"message":"conditional annotation","patches":[{"op":"add","path":"/metadata/annotations/foo","value":"bar"}]}}]}`), &expected)

	if err != nil {
		panic(err)
	}
	allowed, _, bytes, err := createPatchFromOPAResult(expected.Result)
	if err != nil {
		t.Fatal(err)
	}
	if bytes == nil || len(bytes) == 0 {
		t.Fatal("bytes is nil or empty")
	}
	if !allowed {
		t.Fatal("allowed is false for mutation")
	}

	var patches *[]types.PatchOperation
	if err := json.Unmarshal(bytes, &patches); err != nil {
		t.Fatal(err)
	}
}

func TestResultBasicValidation(t *testing.T) {
	var expected opatypes.QueryResponseV1
	err := util.UnmarshalJSON([]byte(
		`{"result":[{"id":"ingress-host-fqdn","resolution":{"message":"invalid ingress host fqdn \"acmecorp.com\""}},
					{"id":"ingress-host-fqdn","resolution":{"message":"invalid ingress host fqdn \"acmecorp.com\""}}
				   ]
		 }`), &expected)
	if err != nil {
		panic(err)
	}
	allowed, reason, patchBytes, err := createPatchFromOPAResult(expected.Result)
	if err != nil {
		t.Fatal(err)
	}
	if patchBytes != nil || len(patchBytes) != 0 {
		t.Fatal("bytes is not nil or empty")
	}
	if allowed {
		t.Fatal("allowed is true for policy violation")
	}
	if reason == "" {
		t.Fatal("reason is nil for policy violation")
	}
}

func TestPatchResultEmpty(t *testing.T) {
	var expected opatypes.QueryResponseV1
	err := util.UnmarshalJSON([]byte(`{
	"result":[{"resolution":{}}]
	}`), &expected)
	if err != nil {
		panic(err)
	}
	_, _, bytes, err := createPatchFromOPAResult(expected.Result)
	if err != nil {
		t.Fatal(err)
	}
	if bytes != nil || len(bytes) != 0 {
		t.Fatal("bytes is not nil or empty")
	}
}

func TestSingleAuthorization(t *testing.T) {
	f := newFixture()

	fakeOpa := &opa.FakeOPA{}
	fakeOpa.SetViolation(`apps.*`, opa.MakeDenyObject("anyID", "anyKind", "anyName", "anyNamespace", "anyMessage", nil))
	mgr, _ := manager.New(&rest.Config{}, manager.Options{})
	addWebhooks(mgr, f.server, fakeOpa)

	violationRequest := makeSubjectAccessReview("apiregistration.k8s.io", "v1beta1", "apiservices", "", "create", "", "v1.apps", "admin", []string{"system:authenticated"})
	validRequest := makeSubjectAccessReview("apiregstration.k8s.io", "v1beta1", "apiservices", "", "create", "", "custom.metrics.k8s.io", "admin", []string{"system:authenticated"})

	setup := []tr{
		{"violation", http.MethodPost, "/authorize", violationRequest, 200, `  {
          "apiVersion": "authorization.k8s.io/v1beta1",
          "kind": "SubjectAccessReview",
          "metadata": {
            "creationTimestamp": null
          },
           "spec": {
            "group": [
              "system:authenticated"
            ],
            "resourceAttributes": {
              "group": "apiregistration.k8s.io",
              "name": "v1.apps",
              "resource": "apiservices",
              "verb": "create",
              "version": "v1beta1"
            },
            "user": "admin"
          },
          "status": {
            "allowed": false,
            "denied": true,
            "reason": "[\"anyMessage\"]"
          }
        }`},
		{"valid", http.MethodPost, "/authorize", validRequest, 200, `  {
          "apiVersion": "authorization.k8s.io/v1beta1",
          "kind": "SubjectAccessReview",
          "metadata": {
            "creationTimestamp": null
          },
          "spec": {
            "group": [
              "system:authenticated"
            ],
            "resourceAttributes": {
              "group": "apiregstration.k8s.io",
              "name": "custom.metrics.k8s.io",
              "resource": "apiservices",
              "verb": "create",
              "version": "v1beta1"
            },
            "user": "admin"
          },
          "status": {
            "allowed": false
          }
        }`},
	}

	for _, tr := range setup {
		t.Run(tr.name, func(t *testing.T) {
			req := newReqV1(tr.method, tr.path, tr.body)
			req.RemoteAddr = "testaddr"

			if err := f.executeRequest(req, tr.code, tr.resp); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSingleAuthorizationWithUnparseableSubjectAccessReview(t *testing.T) {
	f := newFixture()

	fakeOpa := &opa.FakeOPA{}
	fakeOpa.SetViolation(`apps.*`, opa.MakeDenyObject("anyID", "anyKind", "anyName", "anyNamespace", "anyMessage", nil))
	mgr, _ := manager.New(&rest.Config{}, manager.Options{})
	addWebhooks(mgr, f.server, fakeOpa)

	violationRequest1 := `{"broken SubjectAccessReview"}`
	violationRequest2 := ``

	setup := []tr{
		{"violation", http.MethodPost, "/authorize", violationRequest1, 200, `  {
          "apiVersion": "authorization.k8s.io/v1beta1",
          "kind": "SubjectAccessReview",
          "metadata": {
            "creationTimestamp": null
          },
          "spec": {},
          "status": {
            "allowed": false,
            "denied": true,
            "evaluationError": "couldn't get version/kind; json parse error: invalid character '}' after object key"
          }
        }`},
		{"violation2", http.MethodPost, "/authorize", violationRequest2, 400, ""},
	}

	for _, tr := range setup {
		t.Run(tr.name, func(t *testing.T) {
			req := newReqV1(tr.method, tr.path, tr.body)
			req.RemoteAddr = "testaddr"

			if err := f.executeRequest(req, tr.code, tr.resp); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMultipleAuthorization(t *testing.T) {
	f := newFixture()

	fakeOpa := &opa.FakeOPA{}
	fakeOpa.SetViolation(`apps.*`, opa.MakeDenyObject("anyID", "anyKind", "anyName", "anyNamespace", "anyMessage", nil),
		opa.MakeDenyObject("anyID", "anyKind", "anyName", "anyNamespace", "anyMessage2", nil))
	mgr, _ := manager.New(&rest.Config{}, manager.Options{})
	addWebhooks(mgr, f.server, fakeOpa)

	violationRequest := makeSubjectAccessReview("apiregistration.k8s.io", "v1beta1", "apiservices", "", "create", "", "v1.apps", "admin", []string{"system:authenticated"})

	setup := []tr{
		{"violation", http.MethodPost, "/authorize", violationRequest, 200, `  {
          "apiVersion": "authorization.k8s.io/v1beta1",
          "kind": "SubjectAccessReview",
          "metadata": {
            "creationTimestamp": null
          },
           "spec": {
            "group": [
              "system:authenticated"
            ],
            "resourceAttributes": {
              "group": "apiregistration.k8s.io",
              "name": "v1.apps",
              "resource": "apiservices",
              "verb": "create",
              "version": "v1beta1"
            },
            "user": "admin"
          },
          "status": {
            "allowed": false,
            "denied": true,
            "reason": "[\"anyMessage\",\"anyMessage2\"]"
          }
        }`},
	}

	for _, tr := range setup {
		t.Run(tr.name, func(t *testing.T) {
			req := newReqV1(tr.method, tr.path, tr.body)
			req.RemoteAddr = "testaddr"

			if err := f.executeRequest(req, tr.code, tr.resp); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestParseOPAResultDeny(t *testing.T) {
	var expected opatypes.QueryResponseV1
	err := util.UnmarshalJSON([]byte(`{"result":[{"id":"conditional-annotation","resolution":{"message":"conditional annotation"}}]}`), &expected)

	if err != nil {
		panic(err)
	}
	allowed, reason, err := parseOPAResult(expected.Result)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("allowed should be false if deny query has matched")
	}
	if reason != "[\"conditional annotation\"]" {
		t.Fatal("reason should contain message from OPAResult")
	}
}

func TestParseOPAResultAllow(t *testing.T) {
	var expected opatypes.QueryResponseV1
	err := util.UnmarshalJSON([]byte(`{}`), &expected)

	if err != nil {
		panic(err)
	}
	allowed, reason, err := parseOPAResult(expected.Result)
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Fatal("allowed should be true if no deny query has matched")
	}
	if reason != "" {
		t.Fatal("reason should be empty")
	}
}

func TestParseOPAResultEmptyDeny(t *testing.T) {
	var expected opatypes.QueryResponseV1
	err := util.UnmarshalJSON([]byte(`{"result":[{"resolution":{}}]}`), &expected)
	if err != nil {
		panic(err)
	}
	allowed, reason, err := parseOPAResult(expected.Result)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("allowed should be false if OPAResult contains a result, even if its empty")
	}
	if reason != "[\"\"]" {
		t.Fatal("allowed is false for mutation")
	}
}

func getPatchBytes(patches []types.PatchOperation) ([]byte, error) {
	bs, err := json.Marshal(patches)
	if err != nil {
		return nil, err
	}
	expectedPatchBytes := make([]byte, base64.URLEncoding.EncodedLen(len(bs)))
	base64.StdEncoding.Encode(expectedPatchBytes, bs)
	return expectedPatchBytes, nil
}

func makeAdmissionRequest(kind, namespace, name string) string {
	req := &v1beta1.AdmissionRequest{UID: "anyUID", Name: name, Namespace: namespace}
	req.Kind.Kind = kind
	req.Resource.Resource = fmt.Sprintf("%ss", kind)
	objectStr := fmt.Sprintf(`{"key": %v}`, rand.Intn(10))
	req.Object.Raw = []byte(objectStr)
	r := &v1beta1.AdmissionReview{Request: req}
	b, err := json.Marshal(r)
	if err != nil {
		panic(fmt.Errorf("Error: %s", err))
	}
	return string(b)
}

func makeSubjectAccessReview(group, version, resource, subResource, verb, namespace, name, user string, groups []string) string {
	sar := &authorizationv1beta1.SubjectAccessReview{
		TypeMeta: v1.TypeMeta{
			APIVersion: "authorization.k8s.io/v1beta1",
			Kind:       "SubjectAccessReview",
		},
		Spec: authorizationv1beta1.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1beta1.ResourceAttributes{
				Group:       group,
				Version:     version,
				Resource:    resource,
				Subresource: subResource,
				Verb:        verb,
				Namespace:   namespace,
				Name:        name,
			},
			User:   user,
			Groups: groups,
		},
	}
	b, err := json.Marshal(sar)
	if err != nil {
		panic(fmt.Errorf("Error: %s", err))
	}
	return string(b)
}

var _ serverLike = &testServer{}

// testServer emulates the registry/routing of webhook.Server
// Ideally this should be replaced with code that relies on the behavior of webhook.Server directly.
type testServer struct {
	sMux *http.ServeMux
}

func (s *testServer) Handle(path string, handler http.Handler) {
	s.sMux.Handle(path, handler)
}

func (s *testServer) Register(webhooks ...webhook.Webhook) error {
	for _, wh := range webhooks {
		s.sMux.Handle(wh.GetPath(), wh.Handler())
	}
	return nil
}

type fixture struct {
	server   *testServer
	recorder *httptest.ResponseRecorder
}

func newFixture() *fixture {
	recorder := httptest.NewRecorder()

	return &fixture{
		server:   &testServer{sMux: http.NewServeMux()},
		recorder: recorder,
	}
}

func (f *fixture) reset() {
	f.recorder = httptest.NewRecorder()
}

func newReqV1(method string, path string, body string) *http.Request {
	return newReq(1, method, path, body)
}

func newReq(version int, method, path, body string) *http.Request {
	return newReqUnversioned(method, fmt.Sprintf("/v%d", version)+path, body)
}

func newReqUnversioned(method, path, body string) *http.Request {
	req, err := http.NewRequest(method, path, strings.NewReader(body))
	if err != nil {
		panic(err)
	}
	req.Header = http.Header{}
	req.Header.Set("Content-Type", "application/json")
	return req
}

func (f *fixture) executeRequest(req *http.Request, code int, resp string) error {
	f.reset()
	f.server.sMux.ServeHTTP(f.recorder, req)
	if f.recorder.Code != code {
		return fmt.Errorf("Expected code %v from %v %v but got: %v", code, req.Method, req.URL, f.recorder)
	}
	if resp != "" {
		var result interface{}
		if err := util.UnmarshalJSON([]byte(f.recorder.Body.String()), &result); err != nil {
			return fmt.Errorf("Expected JSON response from %v %v but got: %v", req.Method, req.URL, f.recorder)
		}
		var expected interface{}
		if err := util.UnmarshalJSON([]byte(resp), &expected); err != nil {
			panic(err)
		}
		if !reflect.DeepEqual(result, expected) {
			a, err := json.MarshalIndent(expected, "", "  ")
			if err != nil {
				panic(err)
			}
			b, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				panic(err)
			}
			return fmt.Errorf("Expected JSON response from %v %v to equal:\n\n%s\n\nGot:\n\n%s", req.Method, req.URL, a, b)
		}
	}
	return nil
}

type tr struct {
	name   string
	method string
	path   string
	body   string
	code   int
	resp   string
}
