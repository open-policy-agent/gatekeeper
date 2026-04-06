package oci

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"

	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry"
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
		return "", nil, fmt.Errorf("creating temporary policy directory under path %q: %w", tempDir, err)
	}
	cleanupPath := func() { os.RemoveAll(path) }

	credStore, err := credentials.NewStoreFromDocker(credentials.StoreOptions{
		DetectDefaultNativeStore: true,
	})
	if err != nil {
		cleanupPath()
		return "", nil, fmt.Errorf("new auth client: %w", err)
	}

	fileStore, err := file.New(path)
	if err != nil {
		cleanupPath()
		return "", nil, fmt.Errorf("creating file store at path %q: %w", path, err)
	}

	closeFn := func() {
		fileStore.Close()
		os.RemoveAll(path)
	}

	ref, err := registry.ParseReference(imgURL)
	if err != nil {
		return "", closeFn, fmt.Errorf("parsing OCI reference %q: %w", imgURL, err)
	}

	repo, err := remote.NewRepository(ref.Registry + "/" + ref.Repository)
	if err != nil {
		return "", closeFn, fmt.Errorf("creating remote repository for %q: %w", imgURL, err)
	}
	repo.PlainHTTP = shouldUsePlainHTTP(ref.Registry)
	repo.Client = &auth.Client{
		Client:     http.DefaultClient,
		Cache:      auth.NewCache(),
		Credential: credentials.Credential(credStore),
	}

	_, err = oras.Copy(ctx, repo, ref.ReferenceOrDefault(), fileStore, "", oras.DefaultCopyOptions)
	if err != nil {
		return "", closeFn, fmt.Errorf("pulling artifact %q: %w", imgURL, err)
	}

	return path, closeFn, nil
}

// shouldUsePlainHTTP returns true when the registry host is a loopback
// address (localhost, 127.x.x.x, ::1), which typically serves plain HTTP.
func shouldUsePlainHTTP(registryHost string) bool {
	host, _, err := net.SplitHostPort(registryHost)
	if err != nil {
		host = registryHost
	}
	// Strip IPv6 brackets that remain when no port is present (e.g. "[::1]").
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	if host == "localhost" {
		return true
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.IsLoopback()
	}
	return false
}
