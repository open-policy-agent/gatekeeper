package oci

import (
	"context"
	"fmt"
	"os"
	"strings"

	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

const tempFilePrefix = "gator-bundle-"

// PullImage pulls an OCI image at `imgURL` into a temporary directory with a
// random name, created under the path `tempDir`. If `tempDir` is empty, a
// default path from os.TempDir() is used. This func returns the directory path
// that the image was pulled to, a handler function to clean up the directory
// after it has been read, and an error (if any).
func PullImage(imgURL string, tempDir string) (string, func(), error) {
	ctx := context.Background()
	path, err := os.MkdirTemp(tempDir, tempFilePrefix)
	if err != nil {
		return "", nil, fmt.Errorf("creating temporary policy directory at path %q: %w", path, err)
	}

	fileStore, err := file.New(path)
	if err != nil {
		os.RemoveAll(path)
		return "", nil, fmt.Errorf("creating file store: %w", err)
	}
	closeFn := func() {
		fileStore.Close()
		os.RemoveAll(path)
	}

	repo, err := remote.NewRepository(imgURL)
	if err != nil {
		return "", closeFn, fmt.Errorf("creating remote repository: %w", err)
	}
	repo.PlainHTTP = isInsecureRegistry(repo.Reference.Registry)

	credStore, err := credentials.NewStoreFromDocker(credentials.StoreOptions{})
	if err != nil {
		return "", closeFn, fmt.Errorf("creating credential store: %w", err)
	}
	repo.Client = &auth.Client{
		Credential: credentials.Credential(credStore),
	}

	tag := repo.Reference.Reference
	_, err = oras.Copy(ctx, repo, tag, fileStore, tag, oras.DefaultCopyOptions)
	if err != nil {
		return "", closeFn, fmt.Errorf("pulling artifact: %w", err)
	}

	return path, closeFn, err
}

// isInsecureRegistry returns true for registries that should use plain HTTP.
func isInsecureRegistry(registry string) bool {
	host := strings.SplitN(registry, ":", 2)[0]
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
