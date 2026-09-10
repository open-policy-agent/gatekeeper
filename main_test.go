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

package main

import (
	"context"
	"flag"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/test/testutils"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var testConfig *rest.Config

func TestMain(m *testing.M) {
	testutils.StartControlPlane(m, &testConfig, 0)
}

func TestSetupControllersStatusOnly(t *testing.T) {
	if err := flag.Set("operation", "status"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := flag.Set("operation", "audit,generate,mutation-controller,mutation-status,mutation-webhook,status,webhook"); err != nil {
			t.Error(err)
		}
	})
	originalExternalDataEnabled := *externaldata.ExternalDataEnabled
	*externaldata.ExternalDataEnabled = false
	t.Cleanup(func() { *externaldata.ExternalDataEnabled = originalExternalDataEnabled })

	mgr, err := ctrl.NewManager(testConfig, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		MapperProvider:         apiutil.NewDynamicRESTMapper,
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker, err := readiness.SetupTrackerNoReadyz(mgr, false, false, false)
	if err != nil {
		t.Fatal(err)
	}

	setupFinished := make(chan struct{})
	close(setupFinished)
	if err := setupControllers(context.Background(), mgr, tracker, setupFinished); err != nil {
		t.Fatalf("setupControllers() = %v, want nil", err)
	}
}
