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

package externaldatastatus

import (
	stdlog "log"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/apis"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var cfg *rest.Config

func TestMain(m *testing.M) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}
	// TODO(ritazh): remove when vap is GAed in k/k
	args := testEnv.ControlPlane.GetAPIServer().Configure()
	args.Append("runtime-config", "api/all=true")

	if err := apis.AddToScheme(scheme.Scheme); err != nil {
		stdlog.Fatal(err)
	}

	// Retrieve the first found binary directory to allow debugging tests from VS Code
	if getFirstFoundEnvTestBinaryDir() != "" {
		testEnv.BinaryAssetsDirectory = getFirstFoundEnvTestBinaryDir()
	}

	var err error
	if cfg, err = testEnv.Start(); err != nil {
		stdlog.Fatal(err)
	}

	if err := testutils.CreateGatekeeperNamespace(cfg); err != nil {
		stdlog.Printf("creating namespace: %v", err)
	}

	code := m.Run()
	if err = testEnv.Stop(); err != nil {
		stdlog.Printf("error while trying to stop server: %v", err)
	}
	os.Exit(code)
}

func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "..", ".tmp", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
