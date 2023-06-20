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

var _ = Describe("Concrete", func() {
	It("should match the only same version", func() {
		ver16 := Concrete{Major: 1, Minor: 16}
		ver17 := Concrete{Major: 1, Minor: 17}
		Expect(ver16.Matches(ver16)).To(BeTrue(), "should match the same version")
		Expect(ver16.Matches(ver17)).To(BeFalse(), "should not match a different version")
	})
	It("should serialize as X.Y.Z", func() {
		Expect(Concrete{Major: 1, Minor: 16, Patch: 3}.String()).To(Equal("1.16.3"))
	})
	Describe("when ordering relative to other versions", func() {
		ver1163 := Concrete{Major: 1, Minor: 16, Patch: 3}
		Specify("newer patch should be newer", func() {
			Expect(ver1163.NewerThan(Concrete{Major: 1, Minor: 16})).To(BeTrue())
		})
		Specify("newer minor should be newer", func() {
			Expect(ver1163.NewerThan(Concrete{Major: 1, Minor: 15, Patch: 3})).To(BeTrue())
		})
		Specify("newer major should be newer", func() {
			Expect(ver1163.NewerThan(Concrete{Major: 0, Minor: 16, Patch: 3})).To(BeTrue())
		})
	})
})

var _ = Describe("Platform", func() {
	Specify("a concrete platform should match exactly itself", func() {
		plat1 := Platform{OS: "linux", Arch: "amd64"}
		plat2 := Platform{OS: "linux", Arch: "s390x"}
		plat3 := Platform{OS: "windows", Arch: "amd64"}
		Expect(plat1.Matches(plat1)).To(BeTrue(), "should match itself")
		Expect(plat1.Matches(plat2)).To(BeFalse(), "should reject a different arch")
		Expect(plat1.Matches(plat3)).To(BeFalse(), "should reject a different os")
	})
	Specify("a wildcard arch should match any arch", func() {
		sel := Platform{OS: "linux", Arch: "*"}
		plat1 := Platform{OS: "linux", Arch: "amd64"}
		plat2 := Platform{OS: "linux", Arch: "s390x"}
		plat3 := Platform{OS: "windows", Arch: "amd64"}
		Expect(sel.Matches(sel)).To(BeTrue(), "should match itself")
		Expect(sel.Matches(plat1)).To(BeTrue(), "should match some arch with the same OS")
		Expect(sel.Matches(plat2)).To(BeTrue(), "should match another arch with the same OS")
		Expect(plat1.Matches(plat3)).To(BeFalse(), "should reject a different os")
	})
	Specify("a wildcard os should match any os", func() {
		sel := Platform{OS: "*", Arch: "amd64"}
		plat1 := Platform{OS: "linux", Arch: "amd64"}
		plat2 := Platform{OS: "windows", Arch: "amd64"}
		plat3 := Platform{OS: "linux", Arch: "s390x"}
		Expect(sel.Matches(sel)).To(BeTrue(), "should match itself")
		Expect(sel.Matches(plat1)).To(BeTrue(), "should match some os with the same arch")
		Expect(sel.Matches(plat2)).To(BeTrue(), "should match another os with the same arch")
		Expect(plat1.Matches(plat3)).To(BeFalse(), "should reject a different arch")
	})
	It("should report a wildcard OS as a wildcard platform", func() {
		Expect(Platform{OS: "*", Arch: "amd64"}.IsWildcard()).To(BeTrue())
	})
	It("should report a wildcard arch as a wildcard platform", func() {
		Expect(Platform{OS: "linux", Arch: "*"}.IsWildcard()).To(BeTrue())
	})
	It("should serialize as os/arch", func() {
		Expect(Platform{OS: "linux", Arch: "amd64"}.String()).To(Equal("linux/amd64"))
	})

	Specify("knows how to produce a base store name", func() {
		plat := Platform{OS: "linux", Arch: "amd64"}
		ver := Concrete{Major: 1, Minor: 16, Patch: 3}
		Expect(plat.BaseName(ver)).To(Equal("1.16.3-linux-amd64"))
	})

	Specify("knows how to produce an archive name", func() {
		plat := Platform{OS: "linux", Arch: "amd64"}
		ver := Concrete{Major: 1, Minor: 16, Patch: 3}
		Expect(plat.ArchiveName(ver)).To(Equal("kubebuilder-tools-1.16.3-linux-amd64.tar.gz"))
	})

	Describe("parsing", func() {
		Context("for version-platform names", func() {
			It("should accept strings of the form x.y.z-os-arch", func() {
				ver, plat := ExtractWithPlatform(VersionPlatformRE, "1.16.3-linux-amd64")
				Expect(ver).To(Equal(&Concrete{Major: 1, Minor: 16, Patch: 3}))
				Expect(plat).To(Equal(Platform{OS: "linux", Arch: "amd64"}))
			})
			It("should reject nonsense strings", func() {
				ver, _ := ExtractWithPlatform(VersionPlatformRE, "1.16-linux-amd64")
				Expect(ver).To(BeNil())
			})
		})
		Context("for archive names", func() {
			It("should accept strings of the form kubebuilder-tools-x.y.z-os-arch.tar.gz", func() {
				ver, plat := ExtractWithPlatform(ArchiveRE, "kubebuilder-tools-1.16.3-linux-amd64.tar.gz")
				Expect(ver).To(Equal(&Concrete{Major: 1, Minor: 16, Patch: 3}))
				Expect(plat).To(Equal(Platform{OS: "linux", Arch: "amd64"}))
			})
			It("should reject nonsense strings", func() {
				ver, _ := ExtractWithPlatform(ArchiveRE, "kubebuilder-tools-1.16.3-linux-amd64.tar.sum")
				Expect(ver).To(BeNil())
			})
		})
	})
})

var _ = Describe("Spec helpers", func() {
	Specify("can fill a spec with a concrete version", func() {
		spec := Spec{Selector: AnySelector{}} // don't just use AnyVersion so we don't modify it
		spec.MakeConcrete(Concrete{Major: 1, Minor: 16})
		Expect(spec.AsConcrete()).To(Equal(&Concrete{Major: 1, Minor: 16}))
	})
	It("should serialize as the underlying selector with ! for check latest", func() {
		spec, err := FromExpr("1.16.*!")
		Expect(err).NotTo(HaveOccurred())
		Expect(spec.String()).To(Equal("1.16.*!"))
	})
	It("should serialize as the underlying selector by itself if not check latest", func() {
		spec, err := FromExpr("1.16.*")
		Expect(err).NotTo(HaveOccurred())
		Expect(spec.String()).To(Equal("1.16.*"))
	})
})
