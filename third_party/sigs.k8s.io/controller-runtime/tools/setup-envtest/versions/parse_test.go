/*
Copyright 2021 The Kubernetes Authors.

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

package versions_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "sigs.k8s.io/controller-runtime/tools/setup-envtest/versions"
)

func patchSel(x, y int, z PointVersion) PatchSelector {
	return PatchSelector{Major: x, Minor: y, Patch: z}
}

func patchSpec(x, y int, z PointVersion) Spec {
	return Spec{Selector: patchSel(x, y, z)}
}

func tildeSel(x, y, z int) TildeSelector {
	return TildeSelector{
		Concrete: Concrete{
			Major: x, Minor: y, Patch: z,
		},
	}
}

func tildeSpec(x, y, z int) Spec {
	return Spec{Selector: tildeSel(x, y, z)}
}
func ltSpec(x, y int, z PointVersion) Spec {
	// this just keeps the table a bit shorter
	return Spec{Selector: LessThanSelector{
		PatchSelector: patchSel(x, y, z),
	}}
}
func lteSpec(x, y int, z PointVersion) Spec {
	// this just keeps the table a bit shorter
	return Spec{Selector: LessThanSelector{
		PatchSelector: patchSel(x, y, z),
		OrEquals:      true,
	}}
}

var _ = Describe("Parse", func() {
	DescribeTable("it should support",
		func(spec string, expected Spec) {
			Expect(FromExpr(spec)).To(Equal(expected))
		},
		Entry("X.Y versions", "1.16", patchSpec(1, 16, AnyPoint)),
		Entry("X.Y.Z versions", "1.16.3", patchSpec(1, 16, PointVersion(3))),
		Entry("X.Y.x wildcard", "1.16.x", patchSpec(1, 16, AnyPoint)),
		Entry("X.Y.* wildcard", "1.16.*", patchSpec(1, 16, AnyPoint)),

		Entry("~X.Y selector", "~1.16", tildeSpec(1, 16, 0)),
		Entry("~X.Y.Z selector", "~1.16.3", tildeSpec(1, 16, 3)),
		Entry("~X.Y.x selector", "~1.16.x", tildeSpec(1, 16, 0)),
		Entry("~X.Y.* selector", "~1.16.*", tildeSpec(1, 16, 0)),

		Entry("<X.Y selector", "<1.16", ltSpec(1, 16, AnyPoint)),
		Entry("<X.Y.Z selector", "<1.16.3", ltSpec(1, 16, PointVersion(3))),
		Entry("<X.Y.x selector", "<1.16.x", ltSpec(1, 16, AnyPoint)),
		Entry("<X.Y.* selector", "<1.16.*", ltSpec(1, 16, AnyPoint)),

		Entry("<=X.Y selector", "<=1.16", lteSpec(1, 16, AnyPoint)),
		Entry("<=X.Y.Z selector", "<=1.16.3", lteSpec(1, 16, PointVersion(3))),
		Entry("<=X.Y.x selector", "<=1.16.x", lteSpec(1, 16, AnyPoint)),
		Entry("<=X.Y.* selector", "<=1.16.*", lteSpec(1, 16, AnyPoint)),

		Entry("X.Y! (latest)", "1.16!", Spec{Selector: patchSel(1, 16, AnyPoint), CheckLatest: true}),
		Entry("X.Y.Z! (latest)", "1.16.3!", Spec{Selector: patchSel(1, 16, PointVersion(3)), CheckLatest: true}),
		Entry("X.Y.x! (latest)", "1.16.x!", Spec{Selector: patchSel(1, 16, AnyPoint), CheckLatest: true}),
		Entry("X.Y.*! (latest)", "1.16.*!", Spec{Selector: patchSel(1, 16, AnyPoint), CheckLatest: true}),
		Entry("~X.Y.*! (latest with selector)", "~1.16.*!", Spec{Selector: tildeSel(1, 16, 0), CheckLatest: true}),
	)

	It("should reject nonsense input", func() {
		_, err := FromExpr("a.b.c")
		Expect(err).To(HaveOccurred())
	})
})
