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

package store_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"io"
	"io/fs"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/afero"

	"sigs.k8s.io/controller-runtime/tools/setup-envtest/store"
	"sigs.k8s.io/controller-runtime/tools/setup-envtest/versions"
)

const (
	fakeStorePath = "/path/to/the/store"
)

var _ = Describe("Store", func() {
	var st *store.Store
	BeforeEach(func() {
		fs := afero.NewMemMapFs()
		fakeStoreFiles(fs, fakeStorePath)
		st = &store.Store{
			Root: afero.NewBasePathFs(fs, fakeStorePath),
		}
	})
	Describe("initialization", func() {
		It("should ensure the repo root exists", func() {
			// remove the old dir
			Expect(st.Root.RemoveAll("")).To(Succeed(), "should be able to remove the store before trying to initialize")

			Expect(st.Initialize(logCtx())).To(Succeed(), "initialization should succeed")
			Expect(st.Root.Stat("k8s")).NotTo(BeNil(), "store's binary dir should exist")
		})

		It("should be fine if the repo root already exists", func() {
			Expect(st.Initialize(logCtx())).To(Succeed())
		})
	})
	Describe("listing items", func() {
		It("should filter results by the given filter, sorted in version order (newest first)", func() {
			sel, err := versions.FromExpr("<=1.16")
			Expect(err).NotTo(HaveOccurred(), "should be able to construct <=1.16 selector")
			Expect(st.List(logCtx(), store.Filter{
				Version:  sel,
				Platform: versions.Platform{OS: "*", Arch: "amd64"},
			})).To(Equal([]store.Item{
				{Version: ver(1, 16, 2), Platform: versions.Platform{OS: "ifonlysingularitywasstillathing", Arch: "amd64"}},
				{Version: ver(1, 16, 1), Platform: versions.Platform{OS: "linux", Arch: "amd64"}},
				{Version: ver(1, 16, 0), Platform: versions.Platform{OS: "linux", Arch: "amd64"}},
				{Version: ver(1, 14, 26), Platform: versions.Platform{OS: "linux", Arch: "amd64"}},
			}))
		})
		It("should skip non-folders in the store", func() {
			Expect(afero.WriteFile(st.Root, "k8s/2.3.6-linux-amd128", []byte{0x01}, fs.ModePerm)).To(Succeed(), "should be able to create a non-store file in the store directory")
			Expect(st.List(logCtx(), store.Filter{
				Version: versions.AnyVersion, Platform: versions.Platform{OS: "linux", Arch: "amd128"},
			})).To(BeEmpty())
		})

		It("should skip non-matching names in the store", func() {
			Expect(st.Root.Mkdir("k8s/somedir-2.3.6-linux-amd128", fs.ModePerm)).To(Succeed(), "should be able to create a non-store file in the store directory")
			Expect(st.List(logCtx(), store.Filter{
				Version: versions.AnyVersion, Platform: versions.Platform{OS: "linux", Arch: "amd128"},
			})).To(BeEmpty())
		})
	})

	Describe("removing items", func() {
		var res []store.Item
		BeforeEach(func() {
			sel, err := versions.FromExpr("<=1.16")
			Expect(err).NotTo(HaveOccurred(), "should be able to construct <=1.16 selector")
			res, err = st.Remove(logCtx(), store.Filter{
				Version:  sel,
				Platform: versions.Platform{OS: "*", Arch: "amd64"},
			})
			Expect(err).NotTo(HaveOccurred(), "should be able to remove <=1.16 & */amd64")
		})
		It("should return all items removed", func() {
			Expect(res).To(ConsistOf(
				store.Item{Version: ver(1, 16, 2), Platform: versions.Platform{OS: "ifonlysingularitywasstillathing", Arch: "amd64"}},
				store.Item{Version: ver(1, 16, 1), Platform: versions.Platform{OS: "linux", Arch: "amd64"}},
				store.Item{Version: ver(1, 16, 0), Platform: versions.Platform{OS: "linux", Arch: "amd64"}},
				store.Item{Version: ver(1, 14, 26), Platform: versions.Platform{OS: "linux", Arch: "amd64"}},
			))
		})
		It("should remove all items matching the given filter from disk", func() {
			Expect(afero.ReadDir(st.Root, "k8s")).NotTo(ContainElements(
				WithTransform(fs.FileInfo.Name, Equal("1.16.2-ifonlysingularitywasstillathing-amd64")),
				WithTransform(fs.FileInfo.Name, Equal("1.16.1-linux-amd64")),
				WithTransform(fs.FileInfo.Name, Equal("1.16.0-linux-amd64")),
				WithTransform(fs.FileInfo.Name, Equal("1.14.26-linux-amd64")),
			))
		})

		It("should leave items that don't match in place", func() {
			Expect(afero.ReadDir(st.Root, "k8s")).To(ContainElements(
				WithTransform(fs.FileInfo.Name, Equal("1.17.9-linux-amd64")),
				WithTransform(fs.FileInfo.Name, Equal("1.16.2-linux-yourimagination")),
				WithTransform(fs.FileInfo.Name, Equal("1.14.26-hyperwarp-pixiedust")),
			))
		})
	})

	Describe("adding items", func() {
		It("should support .tar.gz input", func() {
			Expect(st.Add(logCtx(), newItem, makeFakeArchive(newName))).To(Succeed())
			Expect(st.Has(newItem)).To(BeTrue(), "should have the item after adding it")
		})

		It("should extract binaries from the given archive to a directly to the item's directory, regardless of path", func() {
			Expect(st.Add(logCtx(), newItem, makeFakeArchive(newName))).To(Succeed())

			dirName := newItem.Platform.BaseName(newItem.Version)
			Expect(afero.ReadFile(st.Root, filepath.Join("k8s", dirName, "some-file"))).To(HavePrefix(newName + "some-file"))
			Expect(afero.ReadFile(st.Root, filepath.Join("k8s", dirName, "other-file"))).To(HavePrefix(newName + "other-file"))
		})

		It("should clean up any existing item directory before creating the new one", func() {
			item := localVersions[0]
			Expect(st.Add(logCtx(), item, makeFakeArchive(newName))).To(Succeed())
			Expect(st.Root.Stat(filepath.Join("k8s", item.Platform.BaseName(item.Version)))).NotTo(BeNil(), "new files should exist")
		})
		It("should clean up if it errors before finishing", func() {
			item := localVersions[0]
			Expect(st.Add(logCtx(), item, new(bytes.Buffer))).NotTo(Succeed(), "should fail to extract")
			_, err := st.Root.Stat(filepath.Join("k8s", item.Platform.BaseName(item.Version)))
			Expect(err).To(HaveOccurred(), "the binaries dir for the item should be gone")

		})
	})

	Describe("checking if items are present", func() {
		It("should report that present directories are present", func() {
			Expect(st.Has(localVersions[0])).To(BeTrue())
		})

		It("should report that absent directories are absent", func() {
			Expect(st.Has(newItem)).To(BeFalse())
		})
	})

	Describe("getting the path", func() {
		It("should return the absolute on-disk path of the given item", func() {
			item := localVersions[0]
			Expect(st.Path(item)).To(Equal(filepath.Join(fakeStorePath, "k8s", item.Platform.BaseName(item.Version))))
		})
	})
})

var (
	// keep this sorted.
	localVersions = []store.Item{
		{Version: ver(1, 17, 9), Platform: versions.Platform{OS: "linux", Arch: "amd64"}},
		{Version: ver(1, 16, 2), Platform: versions.Platform{OS: "linux", Arch: "yourimagination"}},
		{Version: ver(1, 16, 2), Platform: versions.Platform{OS: "ifonlysingularitywasstillathing", Arch: "amd64"}},
		{Version: ver(1, 16, 1), Platform: versions.Platform{OS: "linux", Arch: "amd64"}},
		{Version: ver(1, 16, 0), Platform: versions.Platform{OS: "linux", Arch: "amd64"}},
		{Version: ver(1, 14, 26), Platform: versions.Platform{OS: "linux", Arch: "amd64"}},
		{Version: ver(1, 14, 26), Platform: versions.Platform{OS: "hyperwarp", Arch: "pixiedust"}},
	}

	newItem = store.Item{
		Version:  ver(1, 16, 3),
		Platform: versions.Platform{OS: "linux", Arch: "amd64"},
	}

	newName = "kubebuilder-tools-1.16.3-linux-amd64.tar.gz"
)

func ver(major, minor, patch int) versions.Concrete {
	return versions.Concrete{
		Major: major,
		Minor: minor,
		Patch: patch,
	}
}

func makeFakeArchive(magic string) io.Reader {
	out := new(bytes.Buffer)
	gzipWriter := gzip.NewWriter(out)
	tarWriter := tar.NewWriter(gzipWriter)
	Expect(tarWriter.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     "kubebuilder/bin/", // so we can ensure we skip non-files
		Mode:     0777,
	})).To(Succeed())
	for _, fileName := range []string{"some-file", "other-file"} {
		// create fake file contents: magic+fileName+randomBytes()
		var chunk [1024 * 48]byte // 1.5 times our chunk read size in GetVersion
		copy(chunk[:], magic)
		copy(chunk[len(magic):], fileName)
		start := len(magic) + len(fileName)
		if _, err := rand.Read(chunk[start:]); err != nil {
			panic(err)
		}

		// write to kubebuilder/bin/fileName
		err := tarWriter.WriteHeader(&tar.Header{
			Name: "kubebuilder/bin/" + fileName,
			Size: int64(len(chunk[:])),
			Mode: 0777, // so we can check that we fix this later
		})
		if err != nil {
			panic(err)
		}
		_, err = tarWriter.Write(chunk[:])
		if err != nil {
			panic(err)
		}
	}
	tarWriter.Close()
	gzipWriter.Close()

	return out
}

func fakeStoreFiles(fs afero.Fs, dir string) {
	By("making the unpacked directory")
	unpackedBase := filepath.Join(dir, "k8s")
	Expect(fs.Mkdir(unpackedBase, 0755)).To(Succeed())

	By("making some fake (empty) versions")
	for _, item := range localVersions {
		Expect(fs.Mkdir(filepath.Join(unpackedBase, item.Platform.BaseName(item.Version)), 0755)).To(Succeed())
	}
}
