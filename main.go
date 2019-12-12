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
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	k8sCli "sigs.k8s.io/controller-runtime/pkg/client"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

var (
	logLevel    = flag.String("log-level", "INFO", "Minimum log level. For example, DEBUG, INFO, WARNING, ERROR. Defaulted to INFO if unspecified.")
	metricsAddr = flag.String("metrics-addr", ":8080", "The address the metric endpoint binds to.")
	port        = flag.Int("port", 443, "port for the server. defaulted to 443 if unspecified ")
	certDir     = flag.String("cert-dir", "/certs", "The directory where certs are stored, defaults to /certs")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = api.AddToScheme(scheme)

	_ = configv1alpha1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
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

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: *metricsAddr,
		LeaderElection:     false,
		Port:               *port,
		CertDir:            *certDir,
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

	wm := watch.New(mgr.GetConfig())
	if err := mgr.Add(wm); err != nil {
		setupLog.Error(err, "unable to register watch manager to the manager")
		os.Exit(1)
	}

	// Setup all Controllers
	setupLog.Info("Setting up controller")
	if err := controller.AddToManager(mgr, client, wm); err != nil {
		setupLog.Error(err, "unable to register controllers to the manager")
		os.Exit(1)
	}

	setupLog.Info("setting up webhooks")
	if err := webhook.AddToManager(mgr, client); err != nil {
		setupLog.Error(err, "unable to register webhooks to the manager")
		os.Exit(1)
	}

	setupLog.Info("setting up audit")
	if err := audit.AddToManager(mgr, client); err != nil {
		setupLog.Error(err, "unable to register audit to the manager")
		os.Exit(1)
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

	setupLog.Info("starting manager")
	hadError := false
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		hadError = true
	}

	// wm.Pause() blocks until the watch manager has stopped and ensures it does
	// not restart
	if err := wm.Pause(); err != nil {
		setupLog.Error(err, "could not pause watch manager, attempting cleanup anyway")
	}

	// Unfortunately there is no way to block until all child
	// goroutines of the manager have finished, so sleep long
	// enough for dangling reconciles to finish
	// time.Sleep(5 * time.Second)
	time.Sleep(5 * time.Second)

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
