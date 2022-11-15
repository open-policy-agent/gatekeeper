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

func PullImage(imgURL string, tempDir string) (string, func() error, error) {
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
	closeFn := func() error {
		return fileStore.Close()
	}

	_, err = oras.Copy(ctx, registry, imgURL, fileStore, "")
	if err != nil {
		return "", closeFn, fmt.Errorf("pulling artifact: %w", err)
	}

	return path, closeFn, err
}
