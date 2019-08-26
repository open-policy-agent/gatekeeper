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

	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/local"
	"github.com/open-policy-agent/gatekeeper/pkg/apis"
	"github.com/open-policy-agent/gatekeeper/pkg/audit"
	"github.com/open-policy-agent/gatekeeper/pkg/controller"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/upgrade"
	"github.com/open-policy-agent/gatekeeper/pkg/webhook"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"

	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	logLevel = flag.String("log-level", "INFO", "Minimum log level. For example, DEBUG, INFO, WARNING, ERROR. Defaulted to INFO if unspecified.")
)

func main() {

	flag.Parse()
	switch *logLevel {
	case "DEBUG":
		logf.SetLogger(logf.ZapLogger(true))
	case "WARNING", "ERROR":
		setLoggerForProduction()
	case "INFO":
		fallthrough
	default:
		logf.SetLogger(logf.ZapLogger(false))
	}

	log := logf.Log.WithName("entrypoint")

	// Get a config to talk to the apiserver
	log.Info("setting up client for manager")
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "unable to set up client config")
		os.Exit(1)
	}

	// Create a new Cmd to provide shared dependencies and start components
	log.Info("setting up manager")
	mgr, err := manager.New(cfg, manager.Options{})
	if err != nil {
		log.Error(err, "unable to set up overall controller manager")
		os.Exit(1)
	}

	log.Info("Registering Components.")

	// Setup Scheme for all resources
	log.Info("setting up scheme")
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "unable add APIs to scheme")
		os.Exit(1)
	}

	// initialize OPA
	driver := local.New(local.Tracing(false))
	backend, err := opa.NewBackend(opa.Driver(driver))
	if err != nil {
		log.Error(err, "unable to set up OPA backend")
		os.Exit(1)
	}
	client, err := backend.NewClient(opa.Targets(&target.K8sValidationTarget{}))
	if err != nil {
		log.Error(err, "unable to set up OPA client")
	}

	// Setup all Controllers
	log.Info("Setting up controller")
	if err := controller.AddToManager(mgr, client); err != nil {
		log.Error(err, "unable to register controllers to the manager")
		os.Exit(1)
	}

	log.Info("setting up webhooks")
	if err := webhook.AddToManager(mgr, client); err != nil {
		log.Error(err, "unable to register webhooks to the manager")
		os.Exit(1)
	}

	log.Info("setting up audit")
	if err := audit.AddToManager(mgr, client); err != nil {
		log.Error(err, "unable to register audit to the manager")
		os.Exit(1)
	}

	log.Info("setting up upgrade")
	if err := upgrade.AddToManager(mgr); err != nil {
		log.Error(err, "unable to register upgrade to the manager")
		os.Exit(1)
	}

	// Start the Cmd
	log.Info("Starting the Cmd.")
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "unable to run the manager")
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
	zlog := zap.New(zapcore.NewCore(&logf.KubeAwareEncoder{Encoder: enc, Verbose: false}, sink, lvl))
	zlog = zlog.WithOptions(opts...)
	newlogger := zapr.NewLogger(zlog)
	logf.SetLogger(newlogger)
}
