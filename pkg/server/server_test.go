package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	opa "github.com/Azure/kubernetes-policy-controller/pkg/opa"
	types "github.com/Azure/kubernetes-policy-controller/pkg/policies/types"
	opatypes "github.com/open-policy-agent/opa/server/types"
	"github.com/open-policy-agent/opa/util"
	"k8s.io/api/admission/v1beta1"
)

func TestAdmissionBasic(t *testing.T) {
	f := newFixture(t)

	fakeOpa := &opa.FakeOPA{}
	fakeOpa.SetViolation(`anyname.*`, types.MakeDenyObject("anyID", "anyKind", "anyName", "anyNamespace", "anyMessage"))
	f.server, _ = New().
		WithAddresses([]string{":8182"}).
		WithOPA(fakeOpa).
		Init(context.Background())

	violationRequest := makeAdmissionRequest("anyKind", "anyName", "anyNamespace")
	validRequest := makeAdmissionRequest("anyKind", "validName", "validNamespace")

	setup := []tr{
		{http.MethodPost, "/validate", violationRequest, 200, `{"response": {"allowed": false,"status": {"message": "[\n  {\n    \"v\": {\n      \"id\": \"anyID\",\n      \"resource\": {\n        \"kind\": \"anyKind\",\n        \"namespace\": \"anyNamespace\",\n        \"name\": \"anyName\"\n      },\n      \"message\": \"anyMessage\"\n    }\n  }\n]","metadata": {}},"uid": "anyUID"}}`},
		{http.MethodPost, "/validate", validRequest, 200, `{"response": {"allowed": true,"uid": "anyUID"}}`},
	}

	for _, tr := range setup {
		req := newReqV1(tr.method, tr.path, tr.body)
		req.RemoteAddr = "testaddr"

		if err := f.executeRequest(req, tr.code, tr.resp); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPatchResultBasic(t *testing.T) {
	var expected opatypes.QueryResponseV1
	err := util.UnmarshalJSON([]byte(`{
		"result": [{"id":"conditional-annotation","p":[{"op":"add","path":"/metadata/annotations/foo","value":"bar"}]}]
	}`), &expected)
	if err != nil {
		panic(err)
	}
	bytes, err := createPatchFromOPAResult(expected.Result)
	if err != nil {
		t.Fatal(err)
	}
	if bytes == nil || len(bytes) == 0 {
		t.Fatal("bytes is nil or empty")
	}
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

type fixture struct {
	server   *Server
	recorder *httptest.ResponseRecorder
	t        *testing.T
}

func newFixture(t *testing.T) *fixture {
	ctx := context.Background()
	server, err := New().
		WithAddresses([]string{":7925"}).
		Init(ctx)
	if err != nil {
		panic(err)
	}
	recorder := httptest.NewRecorder()

	return &fixture{
		server:   server,
		recorder: recorder,
		t:        t,
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
	f.server.Handler.ServeHTTP(f.recorder, req)
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
	method string
	path   string
	body   string
	code   int
	resp   string
}
