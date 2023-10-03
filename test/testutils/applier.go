package testutils

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

// Applies fixture YAMLs directly under the provided path in alpha-sorted order.
// Does not crawl the directory, instead it only looks at the files present at path.
func ApplyFixtures(path string, cfg *rest.Config) error {
	files, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("reading path %s: %w", path, err)
	}

	c, err := client.New(cfg, client.Options{})
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	sorted := make([]string, 0, len(files))
	for _, entry := range files {
		if entry.IsDir() {
			continue
		}
		sorted = append(sorted, entry.Name())
	}
	sort.StringSlice(sorted).Sort()

	for _, entry := range sorted {
		b, err := os.ReadFile(filepath.Join(path, entry))
		if err != nil {
			return fmt.Errorf("reading file %s: %w", entry, err)
		}

		desired := unstructured.Unstructured{}
		if err := yaml.Unmarshal(b, &desired); err != nil {
			return fmt.Errorf("parsing file %s: %w", entry, err)
		}

		u := unstructured.Unstructured{}
		u.SetGroupVersionKind(desired.GroupVersionKind())
		u.SetName(desired.GetName())
		u.SetNamespace(desired.GetNamespace())
		_, err = controllerutil.CreateOrUpdate(context.Background(), c, &u, func() error {
			resourceVersion := u.GetResourceVersion()
			desired.DeepCopyInto(&u)
			u.SetResourceVersion(resourceVersion)

			return nil
		})
		if err != nil {
			return fmt.Errorf("creating %v %s: %w", u.GroupVersionKind(), u.GetName(), err)
		}
	}

	return nil
}
