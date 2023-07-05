/*
Copyright 2020 The Kubernetes Authors.
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

package config_test

import (
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/config"

	"sigs.k8s.io/controller-runtime/examples/configfile/custom/v1alpha1"
)

var scheme = runtime.NewScheme()

func init() {
	_ = v1alpha1.AddToScheme(scheme)
}

// This example will load a file using Complete with only
// defaults set.
func ExampleFile() {
	// This will load a config file from ./config.yaml
	loader := config.File()
	if _, err := loader.Complete(); err != nil {
		fmt.Println("failed to load config")
		os.Exit(1)
	}
}

// This example will load the file from a custom path.
func ExampleFile_atPath() {
	loader := config.File().AtPath("/var/run/controller-runtime/config.yaml")
	if _, err := loader.Complete(); err != nil {
		fmt.Println("failed to load config")
		os.Exit(1)
	}
}
