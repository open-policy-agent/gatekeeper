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

package env_test

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/afero"

	. "sigs.k8s.io/controller-runtime/tools/setup-envtest/env"
	"sigs.k8s.io/controller-runtime/tools/setup-envtest/store"
	"sigs.k8s.io/controller-runtime/tools/setup-envtest/versions"
)

var _ = Describe("Env", func() {
	// Most of the rest of this is tested e2e via the workflows test,
	// but there's a few things that are easier to test here.  Eventually
	// we should maybe move some of the tests here.
	var (
		env       *Env
		outBuffer *bytes.Buffer
	)
	BeforeEach(func() {
		outBuffer = new(bytes.Buffer)
		env = &Env{
			Out: outBuffer,
			Log: testLog,

			Store: &store.Store{
				// use spaces and quotes to test our quote escaping below
				Root: afero.NewBasePathFs(afero.NewMemMapFs(), "/kb's test store"),
			},

			// shouldn't use these, but just in case
			NoDownload: true,
			FS:         afero.Afero{Fs: afero.NewMemMapFs()},
		}

		env.Version.MakeConcrete(versions.Concrete{
			Major: 1, Minor: 21, Patch: 3,
		})
		env.Platform.Platform = versions.Platform{
			OS: "linux", Arch: "amd64",
		}
	})

	Describe("printing", func() {
		It("should use a manual path if one is present", func() {
			By("using a manual path")
			Expect(env.PathMatches("/otherstore/1.21.4-linux-amd64")).To(BeTrue())

			By("checking that that path is printed properly")
			env.PrintInfo(PrintPath)
			Expect(outBuffer.String()).To(Equal("/otherstore/1.21.4-linux-amd64"))
		})

		Context("as human-readable info", func() {
			BeforeEach(func() {
				env.PrintInfo(PrintOverview)
			})

			It("should contain the version", func() {
				Expect(outBuffer.String()).To(ContainSubstring("/kb's test store/k8s/1.21.3-linux-amd64"))
			})
			It("should contain the path", func() {
				Expect(outBuffer.String()).To(ContainSubstring("1.21.3"))
			})
			It("should contain the platform", func() {
				Expect(outBuffer.String()).To(ContainSubstring("linux/amd64"))
			})

		})
		Context("as just a path", func() {
			It("should print out just the path", func() {
				env.PrintInfo(PrintPath)
				Expect(outBuffer.String()).To(Equal(`/kb's test store/k8s/1.21.3-linux-amd64`))
			})
		})

		Context("as env vars", func() {
			BeforeEach(func() {
				env.PrintInfo(PrintEnv)
			})
			It("should set KUBEBUILDER_ASSETS", func() {
				Expect(outBuffer.String()).To(HavePrefix("export KUBEBUILDER_ASSETS="))
			})
			It("should quote the return path, escaping quotes to deal with spaces, etc", func() {
				Expect(outBuffer.String()).To(HaveSuffix(`='/kb'"'"'s test store/k8s/1.21.3-linux-amd64'` + "\n"))
			})
		})
	})
})
