package v1

import (
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	"k8s.io/apimachinery/pkg/runtime"
)

func addDefaultingFuncs(scheme *runtime.Scheme) error {
	return RegisterDefaults(scheme)
}

func SetDefaults_ConstraintTemplate(obj *ConstraintTemplate) { // nolint:revive // Required exact function name.
	// turn the CT into an unstructured
	un, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		panic("Failed to convert v1 ConstraintTemplate to Unstructured")
	}

	defaulting.Default(un, structuralSchema)

	err = runtime.DefaultUnstructuredConverter.FromUnstructured(un, obj)
	if err != nil {
		panic("Failed to convert Unstructured to v1 ConstraintTemplate")
	}
}
