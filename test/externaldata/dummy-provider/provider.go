package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
)

func main() {
	fmt.Println("starting server...")
	http.HandleFunc("/validate", validate)

	if err := http.ListenAndServe(":8090", nil); err != nil {
		panic(err)
	}
}

const (
	timeout    = 1 * time.Second
	apiVersion = "externaldata.gatekeeper.sh/v1alpha1"
)

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
		if strings.HasSuffix(key.(string), "_systemError") {
			sendResponse(nil, "testing system error", w)
			return
		}

		// check if key contains "error_" to trigger an error
		if strings.HasPrefix(key.(string), "error_") {
			results = append(results, externaldata.Item{
				Key:   key,
				Error: key.(string) + "_invalid",
			})
		} else if !strings.HasSuffix(key.(string), "_valid") {
			// valid key will have "_valid" appended as return value
			results = append(results, externaldata.Item{
				Key:   key,
				Value: key.(string) + "_valid",
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
