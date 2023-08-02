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

var _ = Describe("Selectors", func() {
	Describe("patch", func() {
		var sel Selector
		Context("with any patch", func() {
			BeforeEach(func() {
				var err error
				sel, err = FromExpr("1.16.*")
				Expect(err).NotTo(HaveOccurred())
			})

			It("should match any patch version with the same major & minor", func() {
				Expect(sel.Matches(Concrete{Major: 1, Minor: 16, Patch: 3})).To(BeTrue(), "should match 1.16.3")
				Expect(sel.Matches(Concrete{Major: 1, Minor: 16, Patch: 0})).To(BeTrue(), "should match 1.16.0")
			})

			It("should reject a different major", func() {
				Expect(sel.Matches(Concrete{Major: 2, Minor: 16, Patch: 3})).To(BeFalse(), "should reject 2.16.3")

			})

			It("should reject a different minor", func() {
				Expect(sel.Matches(Concrete{Major: 1, Minor: 17, Patch: 3})).To(BeFalse(), "should reject 1.17.3")
			})

			It("should serialize as X.Y.*", func() {
				Expect(sel.String()).To(Equal("1.16.*"))
			})

			It("should not be concrete", func() {
				Expect(sel.AsConcrete()).To(BeNil())
			})
		})

		Context("with a specific patch", func() {
			BeforeEach(func() {
				var err error
				sel, err = FromExpr("1.16.3")
				Expect(err).NotTo(HaveOccurred())
			})
			It("should match exactly the major/minor/patch", func() {
				Expect(sel.Matches(Concrete{Major: 1, Minor: 16, Patch: 3})).To(BeTrue(), "should match 1.16.3")
			})

			It("should reject a different major", func() {
				Expect(sel.Matches(Concrete{Major: 2, Minor: 16, Patch: 3})).To(BeFalse(), "should reject 2.16.3")

			})

			It("should reject a different minor", func() {
				Expect(sel.Matches(Concrete{Major: 1, Minor: 17, Patch: 3})).To(BeFalse(), "should reject 1.17.3")

			})

			It("should reject a different patch", func() {

				Expect(sel.Matches(Concrete{Major: 1, Minor: 16, Patch: 4})).To(BeFalse(), "should reject 1.16.4")
			})
			It("should serialize as X.Y.Z", func() {
				Expect(sel.String()).To(Equal("1.16.3"))
			})
			It("may be concrete", func() {
				Expect(sel.AsConcrete()).To(Equal(&Concrete{Major: 1, Minor: 16, Patch: 3}))
			})
		})

	})

	Describe("tilde", func() {
		var sel Selector
		BeforeEach(func() {
			var err error
			sel, err = FromExpr("~1.16.3")
			Expect(err).NotTo(HaveOccurred())
		})
		It("should match exactly the major/minor/patch", func() {
			Expect(sel.Matches(Concrete{Major: 1, Minor: 16, Patch: 3})).To(BeTrue(), "should match 1.16.3")
		})

		It("should match a patch greater than the given one, with the same major/minor", func() {
			Expect(sel.Matches(Concrete{Major: 1, Minor: 16, Patch: 4})).To(BeTrue(), "should match 1.16.4")
		})

		It("should reject a patch less than the given one, with the same major/minor", func() {
			Expect(sel.Matches(Concrete{Major: 1, Minor: 16, Patch: 2})).To(BeFalse(), "should reject 1.16.2")

		})

		It("should reject a different major", func() {
			Expect(sel.Matches(Concrete{Major: 2, Minor: 16, Patch: 3})).To(BeFalse(), "should reject 2.16.3")

		})

		It("should reject a different minor", func() {
			Expect(sel.Matches(Concrete{Major: 1, Minor: 17, Patch: 3})).To(BeFalse(), "should reject 1.17.3")

		})

		It("should treat ~X.Y.* as ~X.Y.Z", func() {
			sel, err := FromExpr("~1.16.*")
			Expect(err).NotTo(HaveOccurred())

			Expect(sel.Matches(Concrete{Major: 1, Minor: 16, Patch: 0})).To(BeTrue(), "should match 1.16.0")
			Expect(sel.Matches(Concrete{Major: 1, Minor: 16, Patch: 3})).To(BeTrue(), "should match 1.16.3")
			Expect(sel.Matches(Concrete{Major: 1, Minor: 17, Patch: 0})).To(BeFalse(), "should reject 1.17.0")
		})
		It("should serialize as ~X.Y.Z", func() {
			Expect(sel.String()).To(Equal("~1.16.3"))
		})
		It("should never be concrete", func() {
			Expect(sel.AsConcrete()).To(BeNil())
		})
	})

	Describe("less-than", func() {
		var sel Selector
		BeforeEach(func() {
			var err error
			sel, err = FromExpr("<1.16.3")
			Expect(err).NotTo(HaveOccurred())
		})
		It("should reject the exact major/minor/patch", func() {
			Expect(sel.Matches(Concrete{Major: 1, Minor: 16, Patch: 3})).To(BeFalse(), "should reject 1.16.3")

		})
		It("should reject greater patches", func() {
			Expect(sel.Matches(Concrete{Major: 1, Minor: 16, Patch: 4})).To(BeFalse(), "should reject 1.16.4")

		})
		It("should reject greater majors", func() {
			Expect(sel.Matches(Concrete{Major: 2, Minor: 16, Patch: 3})).To(BeFalse(), "should reject 2.16.3")

		})
		It("should reject greater minors", func() {
			Expect(sel.Matches(Concrete{Major: 1, Minor: 17, Patch: 3})).To(BeFalse(), "should reject 1.17.3")

		})

		It("should accept lesser patches", func() {

			Expect(sel.Matches(Concrete{Major: 1, Minor: 16, Patch: 2})).To(BeTrue(), "should accept 1.16.2")
		})
		It("should accept lesser majors", func() {
			Expect(sel.Matches(Concrete{Major: 0, Minor: 16, Patch: 3})).To(BeTrue(), "should accept 0.16.3")

		})
		It("should accept lesser minors", func() {
			Expect(sel.Matches(Concrete{Major: 1, Minor: 15, Patch: 3})).To(BeTrue(), "should accept 1.15.3")

		})
		It("should serialize as <X.Y.Z", func() {
			Expect(sel.String()).To(Equal("<1.16.3"))
		})
		It("should never be concrete", func() {
			Expect(sel.AsConcrete()).To(BeNil())
		})
		Context("or-equals", func() {
			var sel Selector
			BeforeEach(func() {
				var err error
				sel, err = FromExpr("<=1.16.3")
				Expect(err).NotTo(HaveOccurred())
			})
			It("should accept the exact major/minor/patch", func() {
				Expect(sel.Matches(Concrete{Major: 1, Minor: 16, Patch: 3})).To(BeTrue(), "should accept 1.16.3")

			})
			It("should serialize as <=X.Y.Z", func() {
				Expect(sel.String()).To(Equal("<=1.16.3"))
			})
			It("should never be concrete", func() {
				Expect(sel.AsConcrete()).To(BeNil())
			})
		})
	})

	Describe("any", func() {
		It("should match any version", func() {
			sel := AnyVersion
			Expect(sel.Matches(Concrete{Major: 1, Minor: 16, Patch: 0})).To(BeTrue(), "should match 1.16.0")
			Expect(sel.Matches(Concrete{Major: 1, Minor: 19, Patch: 3})).To(BeTrue(), "should match 1.19.3")
			Expect(sel.Matches(Concrete{Major: 2, Minor: 10, Patch: 16})).To(BeTrue(), "should match 2.10.6")
		})
		It("should serialize as something without panicing", func() {
			// it should have a form, but we don't specify what it is ATM cause it's not parsable
			Expect(AnyVersion.String()).NotTo(BeEmpty())
		})
		It("should never be concrete", func() {
			Expect(AnyVersion.AsConcrete()).To(BeNil())
		})
	})
})
