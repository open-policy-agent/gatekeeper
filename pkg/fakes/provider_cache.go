package fakes

import (
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1alpha1"
	frameworksexternaldata "github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ExternalDataProviderName is the name of the fake external data provider.
	ExternalDataProviderName = "test-provider"
)

// ExternalDataProviderCache is the cache of external data providers.
var ExternalDataProviderCache = frameworksexternaldata.NewCache()

func init() {
	_ = ExternalDataProviderCache.Upsert(&v1alpha1.Provider{
		ObjectMeta: metav1.ObjectMeta{
			Name: ExternalDataProviderName,
		},
		Spec: v1alpha1.ProviderSpec{
			URL:                   "http://localhost:8080/validate",
			Timeout:               1,
			InsecureTLSSkipVerify: true,
		},
	})
}
