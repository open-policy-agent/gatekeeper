package local

import (
	"net/http"

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

		clientCert, err := d.getTLSCertificate()
		if err != nil {
			return externaldata.HandleError(http.StatusBadRequest, err)
		}

		externaldataResponse, statusCode, err := d.sendRequestToProvider(bctx.Context, &provider, regoReq.Keys, clientCert)
		if err != nil {
			return externaldata.HandleError(statusCode, err)
		}

		regoResponse := externaldata.NewRegoResponse(statusCode, externaldataResponse)
		return externaldata.PrepareRegoResponse(regoResponse)
	}
}
