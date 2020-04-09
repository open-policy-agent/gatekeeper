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
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/zapr"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	"github.com/open-policy-agent/gatekeeper/api"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/api/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/pkg/audit"
	"github.com/open-policy-agent/gatekeeper/pkg/controller"
	configController "github.com/open-policy-agent/gatekeeper/pkg/controller/config"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/constrainttemplate"
	"github.com/open-policy-agent/gatekeeper/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/upgrade"
	"github.com/open-policy-agent/gatekeeper/pkg/watch"
	"github.com/open-policy-agent/gatekeeper/pkg/webhook"
	"github.com/open-policy-agent/gatekeeper/third_party/sigs.k8s.io/controller-runtime/pkg/dynamiccache"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	k8sCli "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	scheme     = runtime.NewScheme()
	setupLog   = ctrl.Log.WithName("setup")
	operations = newOperationSet()
)

var (
	logLevel    = flag.String("log-level", "INFO", "Minimum log level. For example, DEBUG, INFO, WARNING, ERROR. Defaulted to INFO if unspecified.")
	healthAddr  = flag.String("health-addr", ":9090", "The address to which the health endpoint binds.")
	metricsAddr = flag.String("metrics-addr", "0", "The address the metric endpoint binds to.")
	port        = flag.Int("port", 443, "port for the server. defaulted to 443 if unspecified ")
	certDir     = flag.String("cert-dir", "/certs", "The directory where certs are stored, defaults to /certs")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = api.AddToScheme(scheme)

	_ = configv1alpha1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
	flag.Var(operations, "operation", "The operation to be performed by this instance. e.g. audit, webhook. This flag can be declared more than once. Omitting will default to supporting all operations.")
}

type opSet map[string]bool

var _ flag.Value = opSet{}

func newOperationSet() opSet {
	return make(map[string]bool)
}

func (l opSet) String() string {
	contents := make([]string, 0)
	for k := range l {
		contents = append(contents, k)
	}
	return fmt.Sprintf("%s", contents)
}

func (l opSet) Set(s string) error {
	l[s] = true
	return nil
}

func main() {
	flag.Parse()

	switch *logLevel {
	case "DEBUG":
		ctrl.SetLogger(crzap.Logger(true))
	case "WARNING", "ERROR":
		setLoggerForProduction()
	case "INFO":
		fallthrough
	default:
		ctrl.SetLogger(crzap.Logger(false))
	}
	ctrl.SetLogger(crzap.Logger(true))

	// set default if --operation is not provided
	if len(operations) == 0 {
		operations["audit"] = true
		operations["webhook"] = true
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		NewCache:               dynamiccache.New,
		Scheme:                 scheme,
		MetricsBindAddress:     *metricsAddr,
		LeaderElection:         false,
		Port:                   *port,
		CertDir:                *certDir,
		HealthProbeBindAddress: *healthAddr,
		MapperProvider: func(c *rest.Config) (meta.RESTMapper, error) {
			return apiutil.NewDynamicRESTMapper(c)
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// initialize OPA
	driver := local.New(local.Tracing(false))
	backend, err := opa.NewBackend(opa.Driver(driver))
	if err != nil {
		setupLog.Error(err, "unable to set up OPA backend")
		os.Exit(1)
	}
	client, err := backend.NewClient(opa.Targets(&target.K8sValidationTarget{}))
	if err != nil {
		setupLog.Error(err, "unable to set up OPA client")
	}

	c := mgr.GetCache()
	dc, ok := c.(watch.RemovableCache)
	if !ok {
		err := fmt.Errorf("expected dynamic cache, got: %T", c)
		setupLog.Error(err, "fetching dynamic cache")
		os.Exit(1)
	}
	wm, err := watch.New(dc)
	if err != nil {
		setupLog.Error(err, "unable to create watch manager")
		os.Exit(1)
	}
	if err := mgr.Add(wm); err != nil {
		setupLog.Error(err, "unable to register watch manager to the manager")
		os.Exit(1)
	}

	// ControllerSwitch will be used to disable controllers during our teardown process,
	// avoiding conflicts in finalizer cleanup.
	sw := watch.NewSwitch()

	// Setup all Controllers
	setupLog.Info("setting up controller")
	if err := controller.AddToManager(mgr, client, wm, sw); err != nil {
		setupLog.Error(err, "unable to register controllers to the manager")
		os.Exit(1)
	}
	if operations["webhook"] {
		setupLog.Info("setting up webhooks")
		if err := webhook.AddToManager(mgr, client); err != nil {
			setupLog.Error(err, "unable to register webhooks to the manager")
			os.Exit(1)
		}
	}
	if operations["audit"] {
		setupLog.Info("setting up audit")
		if err := audit.AddToManager(mgr, client); err != nil {
			setupLog.Error(err, "unable to register audit to the manager")
			os.Exit(1)
		}
	}

	setupLog.Info("setting up upgrade")
	if err := upgrade.AddToManager(mgr); err != nil {
		setupLog.Error(err, "unable to register upgrade to the manager")
		os.Exit(1)
	}

	setupLog.Info("setting up metrics")
	if err := metrics.AddToManager(mgr); err != nil {
		setupLog.Error(err, "unable to register metrics to the manager")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddReadyzCheck("default", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to create ready check")
		os.Exit(1)
	}
	if err := mgr.AddHealthzCheck("default", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to create health check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	hadError := false
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		hadError = true
	}

	// Manager stops controllers asynchronously.
	// Instead, we use ControllerSwitch to synchronously prevent them from doing more work.
	// This can be removed when finalizer and status teardown is removed.
	setupLog.Info("disabling controllers...")
	sw.Stop()

	// Create a fresh client to be sure RESTmapper is up-to-date
	setupLog.Info("cleaning state...")
	cli, err := k8sCli.New(mgr.GetConfig(), k8sCli.Options{Scheme: mgr.GetScheme(), Mapper: nil})
	if err != nil {
		setupLog.Error(err, "unable to create cleanup client")
		os.Exit(1)
	}

	// Clean up sync finalizers
	// This logic should be disabled if OPA is run as a sidecar
	syncCleaned := make(chan struct{})
	go configController.TearDownState(cli, syncCleaned)

	// Clean up constraint finalizers
	templatesCleaned := make(chan struct{})
	go constrainttemplate.TearDownState(cli, templatesCleaned)

	<-syncCleaned
	<-templatesCleaned
	setupLog.Info("state cleaned")
	if hadError {
		/// give the cert manager time to generate the cert
		time.Sleep(5 * time.Second)
		os.Exit(1)
	}
}

func setLoggerForProduction() {
	sink := zapcore.AddSync(os.Stderr)
	var opts []zap.Option
	encCfg := zap.NewProductionEncoderConfig()
	enc := zapcore.NewJSONEncoder(encCfg)
	lvl := zap.NewAtomicLevelAt(zap.WarnLevel)
	opts = append(opts, zap.AddStacktrace(zap.ErrorLevel),
		zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewSampler(core, time.Second, 100, 100)
		}))
	opts = append(opts, zap.AddCallerSkip(1), zap.ErrorOutput(sink))
	zlog := zap.New(zapcore.NewCore(&crzap.KubeAwareEncoder{Encoder: enc, Verbose: false}, sink, lvl))
	zlog = zlog.WithOptions(opts...)
	newlogger := zapr.NewLogger(zlog)
	ctrl.SetLogger(newlogger)
}
