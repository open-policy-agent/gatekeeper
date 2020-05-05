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

package watch_test

import (
	stdlog "log"
	"os"
	"sync"
	"testing"

	"github.com/onsi/gomega"
	"github.com/open-policy-agent/gatekeeper/api"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var cfg *rest.Config

func TestMain(m *testing.M) {
	t := &envtest.Environment{}
	if err := api.AddToScheme(scheme.Scheme); err != nil {
		stdlog.Fatal(err)
	}
	if err := apiextensionsv1beta1.AddToScheme(scheme.Scheme); err != nil {
		stdlog.Fatal(err)
	}

	var err error
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

// StartTestManager adds recFn
func StartTestManager(mgr manager.Manager, g *gomega.GomegaWithT) (chan struct{}, *sync.WaitGroup) {
	stop := make(chan struct{})
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		g.Expect(mgr.Start(stop)).NotTo(gomega.HaveOccurred())
	}()
	return stop, wg
}
