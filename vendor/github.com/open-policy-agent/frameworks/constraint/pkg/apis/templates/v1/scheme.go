package v1

import (
	ctschema "github.com/open-policy-agent/frameworks/constraint/pkg/schema"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apimachinery/pkg/runtime"
)

const version = "v1"

var (
	structuralSchema *schema.Structural
	versionedScheme  *runtime.Scheme
)

func init() {
	// Prevent problems with ordering of init() function calls.  These
	// functions are called according to the lexicographic order of their
	// containing files.  As Register() is called on the localSchemeBuilder by
	// zz_generated.conversion.go, the conversion functions haven't been
	// registered with the localSchemeBuilder by the time this init() function
	// runs.  We sidestep this problem by adding RegisterConversions here.
	sb := runtime.NewSchemeBuilder(SchemeBuilder.AddToScheme, addDefaultingFuncs)
	sb.Register(RegisterConversions)

	versionedScheme = runtime.NewScheme()
	var err error
	if err = apiextensionsv1.AddToScheme(versionedScheme); err != nil {
		panic(err)
	}
	if err = apiextensions.AddToScheme(versionedScheme); err != nil {
		panic(err)
	}
	if err = sb.AddToScheme(versionedScheme); err != nil {
		panic(err)
	}
	if structuralSchema, err = ctschema.CRDSchema(versionedScheme, version); err != nil {
		panic(err)
	}
}
