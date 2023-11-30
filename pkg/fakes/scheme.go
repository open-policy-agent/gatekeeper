package fakes

import (
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
)

// test scheme for various needs throughout gatekeeper.
var testScheme *runtime.Scheme

func init() {
	testScheme = runtime.NewScheme()
	if err := v1beta1.AddToScheme(testScheme); err != nil {
		panic(err)
	}
}

func GetTestScheme() *runtime.Scheme {
	return testScheme
}
