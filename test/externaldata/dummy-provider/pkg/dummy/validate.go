package dummy

import (
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

type Cache struct {
	cache map[string]bool
}

// NewCache creates a new cache.
func NewCache() *Cache {
	return &Cache{
		cache: make(map[string]bool),
	}
}

func (c *Cache) Validate(w http.ResponseWriter, req *http.Request) {
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
		}

		// check if key contains "error_" to trigger a system error
		if strings.HasPrefix(key, "error_") {
			results = append(results, externaldata.Item{
				Key:   key,
				Value: key + "_invalid",
			})
		}

		// valid key will have "_test" appended as return value
		if !strings.HasSuffix(key, "_test") {
			results = append(results, externaldata.Item{
				Key:   key,
				Value: key + "_test",
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

// // extractImageTag extracts the image tag from the image name.
// // this is meant for testing purposes.
// func extractImageTag(ctx context.Context, image string) (string, error) {
// 	tagSplit := strings.Split(image, ":")
// 	if len(tagSplit) < 2 {
// 		return "", fmt.Errorf("invalid image: %s", image)
// 	}
// 	return tagSplit[1], nil
// }
