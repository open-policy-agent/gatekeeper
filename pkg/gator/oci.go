package gator

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"oras.land/oras-go/pkg/auth"
	dockerauth "oras.land/oras-go/pkg/auth/docker"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"
)

const tempFilePrefix = "gator-bundle-"
const nameConflictMsg = "WARNING - Resource named %q (image: %q) is already defined in image %q"

// LoadImage pulls an OCI Artifact from `url` into a local temporary directory,
// created at `tempDir` if specified. All files in the dir are converted into
// unstructured types, and returned. If a file is encountered which cannot be
// read, this func fails closed. If tempDir is not specified, a system default
// provided by os.TempDir() is used
func loadImage(ctx context.Context, imgUrl string, tempDir string) ([]*unstructured.Unstructured, error) {
	path, err := os.MkdirTemp(tempDir, tempFilePrefix)
	if err != nil {
		return nil, fmt.Errorf("creating temporary policy directory at path %q: %w", path, err)
	}

	cli, err := dockerauth.NewClient()
	if err != nil {
		return nil, fmt.Errorf("new auth client: %w", err)
	}

	opts := []auth.ResolverOption{auth.WithResolverClient(http.DefaultClient)}
	resolver, err := cli.ResolverWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("docker resolver: %w", err)
	}

	registry := content.Registry{Resolver: resolver}

	fileStore := content.NewFile(path)
	defer fileStore.Close()

	_, err = oras.Copy(ctx, registry, imgUrl, fileStore, "")
	if err != nil {
		return nil, fmt.Errorf("pulling artifact: %w", err)
	}

	unstructs, err := ReadFiles([]string{path})
	return unstructs, err
}

func LoadImages(imgURLs []string, tempDir string) ([]*unstructured.Unstructured, error) {
	ctx := context.Background()
	cd := newConflictDetector()
	var objs []*unstructured.Unstructured

	for _, imgURL := range imgURLs {
		imgObjs, err := loadImage(ctx, imgURL, tempDir)
		if err != nil {
			return nil, fmt.Errorf("loading image %s: %s", imgURL, err)
		}
		for _, o := range imgObjs {
			cd.checkConflict(o.GetName(), imgURL)
		}
		objs = append(objs, imgObjs...)
	}

	return objs, nil
}

type conflictDetector struct {
	objs map[string]string // metadata.name -> imgURL
}

func newConflictDetector() *conflictDetector {
	return &conflictDetector{make(map[string]string)}
}

// checkConflicts checks for duplicated resource names, and logs them if a
// conflict is found
func (cd *conflictDetector) checkConflict(objName, imgURL string) {
	if dupe, exists := cd.objs[objName]; exists {
		warningMsg := fmt.Sprintf(nameConflictMsg, objName, imgURL, dupe)
		fmt.Printf(warningMsg)
	}
	cd.objs[objName] = imgURL
}
