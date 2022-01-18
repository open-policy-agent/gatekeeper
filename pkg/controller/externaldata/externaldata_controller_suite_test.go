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

package externaldata

import (
	"context"
	stdlog "log"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-policy-agent/gatekeeper/apis"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var cfg *rest.Config

func TestMain(m *testing.M) {
	var err error

	t := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "vendor", "github.com", "open-policy-agent", "frameworks", "constraint", "deploy", "crds.yaml"),
			filepath.Join("..", "..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}
	if err := apis.AddToScheme(scheme.Scheme); err != nil {
		stdlog.Fatal(err)
	}

	if cfg, err = t.Start(); err != nil {
		stdlog.Fatal(err)
	}
	stdlog.Print("STARTED")

	code := m.Run()
	if err = t.Stop(); err != nil {
		stdlog.Printf("error while trying to stop server: %v", err)
	}
	os.Exit(code)
}

// SetupTestReconcile returns a reconcile.Reconcile implementation that delegates to inner and
// writes the request to requests after Reconcile is finished.
func SetupTestReconcile(inner reconcile.Reconciler) (reconcile.Reconciler, chan reconcile.Request) {
	requests := make(chan reconcile.Request)
	fn := reconcile.Func(func(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
		result, err := inner.Reconcile(ctx, req)
		requests <- req
		return result, err
	})
	return fn, requests
}
