/*

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

package readiness_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

// Applies fixture YAMLs directly under the provided path in alpha-sorted order.
func applyFixtures(path string) error {
	files, err := ioutil.ReadDir(path)
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
		b, err := ioutil.ReadFile(filepath.Join(path, entry))
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
