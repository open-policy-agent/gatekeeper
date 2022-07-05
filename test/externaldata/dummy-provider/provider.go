package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
)

const (
	timeout    = 1 * time.Second
	apiVersion = "externaldata.gatekeeper.sh/v1alpha1"
)

func main() {
	fmt.Println("starting server...")

	// load Gatekeeper's CA certificate
	caCert, err := ioutil.ReadFile("/tmp/gatekeeper/ca.crt")
	if err != nil {
		panic(err)
	}

	clientCAs := x509.NewCertPool()
	clientCAs.AppendCertsFromPEM(caCert)

	mux := http.NewServeMux()
	mux.HandleFunc("/validate", processTimeout(validate, timeout))

	server := &http.Server{
		Addr:    ":8090",
		Handler: mux,
		TLSConfig: &tls.Config{
			ClientAuth: tls.RequireAndVerifyClientCert,
			ClientCAs:  clientCAs,
			MinVersion: tls.VersionTLS13,
		},
	}

	if err := server.ListenAndServeTLS("/etc/ssl/certs/server.crt", "/etc/ssl/certs/server.key"); err != nil {
		panic(err)
	}
}

func validate(w http.ResponseWriter, req *http.Request) {
	// only accept POST requests
	if req.Method != http.MethodPost {
		sendResponse(nil, "only POST is allowed", w)
		return
	}

	// read request body
	requestBody, err := ioutil.ReadAll(req.Body)
	if err != nil {
		sendResponse(nil, fmt.Sprintf("unable to read request body: %v", err), w)
		return
	}

	// parse request body
	var providerRequest externaldata.ProviderRequest
	err = json.Unmarshal(requestBody, &providerRequest)
	if err != nil {
		sendResponse(nil, fmt.Sprintf("unable to unmarshal request body: %v", err), w)
		return
	}

	results := make([]externaldata.Item, 0)
	// iterate over all keys
	for _, key := range providerRequest.Request.Keys {
		// Providers should add a caching mechanism to avoid extra calls to external data sources.

		// following checks are for testing purposes only
		// check if key contains "_systemError" to trigger a system error
		if strings.HasSuffix(key, "_systemError") {
			sendResponse(nil, "testing system error", w)
			return
		}

		// check if key contains "error_" to trigger an error
		if strings.HasPrefix(key, "error_") {
			results = append(results, externaldata.Item{
				Key:   key,
				Error: key + "_invalid",
			})
		} else if !strings.HasSuffix(key, "_valid") {
			// valid key will have "_valid" appended as return value
			results = append(results, externaldata.Item{
				Key:   key,
				Value: key + "_valid",
			})
		}
	}
	sendResponse(&results, "", w)
}

// sendResponse sends back the response to Gatekeeper.
func sendResponse(results *[]externaldata.Item, systemErr string, w http.ResponseWriter) {
	response := externaldata.ProviderResponse{
		APIVersion: apiVersion,
		Kind:       "ProviderResponse",
		Response: externaldata.Response{
			Idempotent: true,
		},
	}

	if results != nil {
		response.Response.Items = *results
	} else {
		response.Response.SystemError = systemErr
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		panic(err)
	}
}

func processTimeout(h http.HandlerFunc, duration time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), duration)
		defer cancel()

		r = r.WithContext(ctx)

		processDone := make(chan bool)
		go func() {
			h(w, r)
			processDone <- true
		}()

		select {
		case <-ctx.Done():
			sendResponse(nil, "operation timed out", w)
		case <-processDone:
		}
	}
}
