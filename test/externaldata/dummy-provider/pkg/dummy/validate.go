package dummy

import (
	"context"
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

	// set context timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	results := make([]externaldata.Item, 0)
	// iterate over all keys
	for _, key := range providerRequest.Request.Keys {
		// check if the key is already in the cache
		if _, ok := c.cache[key.(string)]; ok {
			results = append(results, externaldata.Item{
				Key:   key,
				Value: true,
			})
			continue
		}

		// extract image tag
		tag, err := extractImageTag(ctx, key.(string))
		if err != nil {
			sendResponse(nil, fmt.Sprintf("error while checking image: %v", err), w)
			return
		}

		// check if tag is latest
		if tag != "latest" {
			results = append(results, externaldata.Item{
				Key:   key.(string),
				Value: true,
			})
			// add key to cache
			c.cache[key.(string)] = true
		} else {
			results = append(results, externaldata.Item{
				Key:   key.(string),
				Error: "image tag cannot be latest",
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

// extractImageTag extracts the image tag from the image name.
// this is meant for testing purposes.
func extractImageTag(ctx context.Context, image string) (string, error) {
	tagSplit := strings.Split(image, ":")
	if len(tagSplit) < 2 {
		return "", fmt.Errorf("invalid image: %s", image)
	}
	return tagSplit[1], nil
}
