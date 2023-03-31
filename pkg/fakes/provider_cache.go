package fakes

import (
	externaldataUnversioned "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/unversioned"
	frameworksexternaldata "github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ExternalDataProviderName is the name of the fake external data provider.
	ExternalDataProviderName = "test-provider"
)

// ExternalDataProviderCache is the cache of external data providers.
var ExternalDataProviderCache = frameworksexternaldata.NewCache()

func init() {
	_ = ExternalDataProviderCache.Upsert(&externaldataUnversioned.Provider{
		ObjectMeta: metav1.ObjectMeta{
			Name: ExternalDataProviderName,
		},
		Spec: externaldataUnversioned.ProviderSpec{
			URL:      "https://localhost:8080/validate",
			Timeout:  1,
			CABundle: util.ValidCABundle,
		},
	})
}
