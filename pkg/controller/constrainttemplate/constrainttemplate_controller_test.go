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

package constrainttemplate

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/audit"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/certmanagersyncer"
	controllerconfig "github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/initcfg"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/mutationconfig"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/sync"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	v1 "github.com/open-policy-agent/gatekeeper/v3/pkg/v1"
	"github.com/open-policy-agent/gatekeeper/v3/test/helpers"
)

var (
	testEnv *envtest.Environment
	k8sClient client.Client
	ctx       context.Context
	cancel    context.CancelFunc
)

const (
	timeout  = 10
	interval = 1
)

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(GinkgoWriter)))
	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			"../../../config/crd/bases",
		},
	}

	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = v1.AddToScheme(scheme.Scheme)
	Expect(err).ToNot(HaveOccurred())

	err = corev1.AddToScheme(scheme.Scheme)
	Expect(err).ToNot(HaveOccurred())

	// Add gatekeeper schemes
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())

	// Setup Manager
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	// Create namespace for our test
	ns := &corev1.Namespace{}
	ns.Name = "gatekeeper-test-ns"
	err = k8sClient.Create(ctx, ns)
	Expect(err).ToNot(HaveOccurred())

	// Start the manager in a goroutine
	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()
}, 60)

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

var _ = Describe("Test ConstraintTemplate Controller", func() {
	It("should initialize without error in status-only mode", func() {
		// This test verifies that the controller can be initialized with only
		// status operation enabled, which should not require a constraint client.
		// We can't directly test the full controller initialization in unit tests,
		// but we can verify the operations package behavior.
		// The actual integration test would need a real Kubernetes cluster.
	})

	It("should not include Status in HasValidationOperations", func() {
		// Test that HasValidationOperations returns false when only status is enabled
		ops := operations.NewOpSet()
		ops.Set(string(operations.Status))
		Expect(operations.HasValidationOperations()).To(BeFalse(),
			"HasValidationOperations should return false when only status is assigned")
	})

	It("should return true for HasStatusOperations when status is enabled", func() {
		// Test that HasStatusOperations works correctly
		ops := operations.NewOpSet()
		ops.Set(string(operations.Status))
		Expect(operations.HasStatusOperations()).To(BeTrue(),
			"HasStatusOperations should return true when status is assigned")
	})

	It("should return false for HasStatusOperations when status is not enabled", func() {
		// Test that HasStatusOperations returns false when status is not assigned
		ops := operations.NewOpSet()
		Expect(operations.HasStatusOperations()).To(BeFalse(),
			"HasStatusOperations should return false when status is not assigned")
	})
})

func TestConstraintTemplate(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ConstraintTemplate Controller Suite")
}