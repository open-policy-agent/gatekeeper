package manager

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/config"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	configv1alpha1 "sigs.k8s.io/controller-runtime/pkg/config/v1alpha1"
)

var _ = Describe("manager.Options", func() {
	Describe("AndFrom", func() {
		Describe("reading custom type using OfKind", func() {
			var (
				o   Options
				c   customConfig
				err error
			)

			JustBeforeEach(func() {
				s := runtime.NewScheme()
				o = Options{Scheme: s}
				c = customConfig{}

				_, err = o.AndFrom(config.File().AtPath("./testdata/custom-config.yaml").OfKind(&c))
			})

			It("should not panic or fail", func() {
				Expect(err).To(Succeed())
			})
			It("should set custom properties", func() {
				Expect(c.CustomValue).To(Equal("foo"))
			})
		})
	})
})

type customConfig struct {
	metav1.TypeMeta                                   `json:",inline"`
	configv1alpha1.ControllerManagerConfigurationSpec `json:",inline"`
	CustomValue                                       string `json:"customValue"`
}

func (in *customConfig) DeepCopyObject() runtime.Object {
	out := &customConfig{}
	*out = *in

	in.ControllerManagerConfigurationSpec.DeepCopyInto(&out.ControllerManagerConfigurationSpec)

	return out
}
