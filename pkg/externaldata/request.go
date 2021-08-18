package externaldata

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"

	externaldatav1alpha1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
)

func SendProviderRequest(provider *externaldatav1alpha1.Provider, providerResponseCache map[types.ProviderCacheKey]string) (map[types.ProviderCacheKey]string, error) {
	body, err := json.Marshal(providerResponseCache)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", provider.Spec.ProxyURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	timeout := time.Second * time.Duration(provider.Spec.Timeout)
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var respBody []byte
	if resp.StatusCode == 200 {
		respBody, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Error(err, "unable to read response body")
			return nil, err
		}
	}

	var result map[types.ProviderCacheKey]string
	err = json.Unmarshal(respBody, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}
