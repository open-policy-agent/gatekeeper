package local

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
)

func externalDataBuiltin(d *Driver) func(bctx rego.BuiltinContext, regorequest *ast.Term) (*ast.Term, error) {
	return func(bctx rego.BuiltinContext, regorequest *ast.Term) (*ast.Term, error) {
		var regoReq externaldata.RegoRequest
		if err := ast.As(regorequest.Value, &regoReq); err != nil {
			return nil, err
		}

		provider, err := d.providerCache.Get(regoReq.ProviderName)
		if err != nil {
			return externaldata.HandleError(http.StatusBadRequest, err)
		}

		externaldataRequest := externaldata.NewProviderRequest(regoReq.Keys)
		reqBody, err := json.Marshal(externaldataRequest)
		if err != nil {
			return externaldata.HandleError(http.StatusInternalServerError, err)
		}

		req, err := http.NewRequest("POST", provider.Spec.URL, bytes.NewBuffer(reqBody))
		if err != nil {
			return externaldata.HandleError(http.StatusInternalServerError, err)
		}
		req.Header.Set("Content-Type", "application/json")

		ctx, cancel := context.WithDeadline(bctx.Context, time.Now().Add(time.Duration(provider.Spec.Timeout)*time.Second))
		defer cancel()

		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			return externaldata.HandleError(http.StatusInternalServerError, err)
		}

		defer func() {
			_ = resp.Body.Close()
		}()

		respBody, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return externaldata.HandleError(http.StatusInternalServerError, err)
		}

		var externaldataResponse externaldata.ProviderResponse
		if err := json.Unmarshal(respBody, &externaldataResponse); err != nil {
			return externaldata.HandleError(http.StatusInternalServerError, err)
		}

		regoResponse := externaldata.NewRegoResponse(resp.StatusCode, &externaldataResponse)
		return externaldata.PrepareRegoResponse(regoResponse)
	}
}
