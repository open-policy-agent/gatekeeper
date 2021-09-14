package templates

import (
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var Scheme *runtime.Scheme

func initializeScheme() {
	Scheme = runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(Scheme); err != nil {
		panic(err)
	}
	if err := apiextensions.AddToScheme(Scheme); err != nil {
		panic(err)
	}
}
