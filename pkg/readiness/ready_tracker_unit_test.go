/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package readiness

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Stub out the lister.
type dummyLister struct{}

var scheme *runtime.Scheme

func init() {
	scheme = runtime.NewScheme()
	if err := v1beta1.AddToScheme(scheme); err != nil {
		panic(err)
	}
}

var testConstraintTemplate = templates.ConstraintTemplate{
	ObjectMeta: v1.ObjectMeta{
		Name: "test-contraint-template",
	},
	Spec: templates.ConstraintTemplateSpec{
		CRD: templates.CRD{
			Spec: templates.CRDSpec{
				Names: templates.Names{
					Kind: "test-constraint",
				},
			},
		},
	},
}

func (dl dummyLister) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if l, ok := list.(*v1beta1.ConstraintTemplateList); ok {
		i := v1beta1.ConstraintTemplate{}
		if err := scheme.Convert(&testConstraintTemplate, &i, nil); err != nil {
			// These failures will be swallowed by readiness.retryAll
			return err
		}
		l.Items = []v1beta1.ConstraintTemplate{i}
	}
	return nil
}

// Verify that TryCancelTemplate functions the same as regular CancelTemplate if readinessRetries is set to 0.
func Test_ReadyTracker_TryCancelTemplate_No_Retries(t *testing.T) {
	g := gomega.NewWithT(t)

	l := dummyLister{}
	rt := newTracker(l, false, func() objData {
		return objData{retries: 0}
	})

	// Run kicks off all the tracking
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		err := rt.Run(ctx)
		if err != nil {
			t.Errorf("Tracker Run() failed with error: %v", err)
		}
	}()
	defer cancel()

	g.Eventually(func() bool {
		return rt.Populated()
	}, "10s").Should(gomega.BeTrue())

	g.Expect(rt.Satisfied(ctx)).NotTo(gomega.BeTrue(), "tracker with 0 retries should not be satisfied")

	rt.TryCancelTemplate(&testConstraintTemplate) // 0 retries --> DELETE

	g.Expect(rt.Satisfied(ctx)).To(gomega.BeTrue(), "tracker with 0 retries and cancellation should be satisfied")
}

// Verify that TryCancelTemplate must be called enough times to remove all retries before canceling a template.
func Test_ReadyTracker_TryCancelTemplate_Retries(t *testing.T) {
	g := gomega.NewWithT(t)

	l := dummyLister{}
	rt := newTracker(l, false, func() objData {
		return objData{retries: 2}
	})

	// Run kicks off all the tracking
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		err := rt.Run(ctx)
		if err != nil {
			t.Errorf("Tracker Run() failed with error: %v", err)
		}
	}()
	defer cancel()

	g.Eventually(func() bool {
		return rt.Populated()
	}, "10s").Should(gomega.BeTrue())

	g.Expect(rt.Satisfied(ctx)).NotTo(gomega.BeTrue(), "tracker with 2 retries should not be satisfied")

	rt.TryCancelTemplate(&testConstraintTemplate) // 2 --> 1 retries

	g.Expect(rt.Satisfied(ctx)).NotTo(gomega.BeTrue(), "tracker with 1 retries should not be satisfied")

	rt.TryCancelTemplate(&testConstraintTemplate) // 1 --> 0 retries

	g.Expect(rt.Satisfied(ctx)).NotTo(gomega.BeTrue(), "tracker with 0 retries should not be satisfied")

	rt.TryCancelTemplate(&testConstraintTemplate) // 0 retries --> DELETE

	g.Expect(rt.Satisfied(ctx)).To(gomega.BeTrue(), "tracker with 0 retries and cancellation should be satisfied")
}
