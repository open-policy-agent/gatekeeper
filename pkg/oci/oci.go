package oci

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"oras.land/oras-go/pkg/auth"
	dockerauth "oras.land/oras-go/pkg/auth/docker"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"
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

	cli, err := dockerauth.NewClient()
	if err != nil {
		return "", nil, fmt.Errorf("new auth client: %w", err)
	}

	opts := []auth.ResolverOption{auth.WithResolverClient(http.DefaultClient)}
	resolver, err := cli.ResolverWithOpts(opts...)
	if err != nil {
		return "", nil, fmt.Errorf("docker resolver: %w", err)
	}

	registry := content.Registry{Resolver: resolver}

	fileStore := content.NewFile(path)
	closeFn := func() {
		fileStore.Close()
		os.RemoveAll(path)
	}

	_, err = oras.Copy(ctx, registry, imgURL, fileStore, "")
	if err != nil {
		return "", closeFn, fmt.Errorf("pulling artifact: %w", err)
	}

	return path, closeFn, err
}
