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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"time"

	"github.com/go-logr/zapr"
	"github.com/open-policy-agent/cert-controller/pkg/rotator"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/k8scel"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	frameworksexternaldata "github.com/open-policy-agent/frameworks/constraint/pkg/externaldata"
	api "github.com/open-policy-agent/gatekeeper/v3/apis"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	expansionv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/expansion/v1alpha1"
	expansionv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/expansion/v1beta1"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/v1alpha1"
	mutationsv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/v1beta1"
	statusv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/audit"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/externaldata"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/readiness/pruner"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/upgrade"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/version"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/webhook"
	_ "go.uber.org/automaxprocs" // set GOMAXPROCS to the number of container cores, if known.
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	crWebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	secretName     = "gatekeeper-webhook-server-cert"
	caName         = "gatekeeper-ca"
	caOrganization = "gatekeeper"
	certName       = "tls.crt"
	keyName        = "tls.key"
)

var (
	scheme           = runtime.NewScheme()
	setupLog         = ctrl.Log.WithName("setup")
	logLevelEncoders = map[string]zapcore.LevelEncoder{
		"lower":        zapcore.LowercaseLevelEncoder,
		"capital":      zapcore.CapitalLevelEncoder,
		"color":        zapcore.LowercaseColorLevelEncoder,
		"capitalcolor": zapcore.CapitalColorLevelEncoder,
	}
)

var (
	logFile                              = flag.String("log-file", "", "Log to file, if specified. Default is to log to stderr.")
	logLevel                             = flag.String("log-level", "INFO", "Minimum log level. For example, DEBUG, INFO, WARNING, ERROR. Defaulted to INFO if unspecified.")
	logLevelKey                          = flag.String("log-level-key", "level", "JSON key for the log level field, defaults to `level`")
	logLevelEncoder                      = flag.String("log-level-encoder", "lower", "Encoder for the value of the log level field. Valid values: [`lower`, `capital`, `color`, `capitalcolor`], default: `lower`")
	healthAddr                           = flag.String("health-addr", ":9090", "The address to which the health endpoint binds.")
	metricsAddr                          = flag.String("metrics-addr", "0", "The address the metric endpoint binds to.")
	port                                 = flag.Int("port", 443, "port for the server. defaulted to 443 if unspecified ")
	host                                 = flag.String("host", "", "the host address the webhook server listens on. defaults to all addresses.")
	certDir                              = flag.String("cert-dir", "/certs", "The directory where certs are stored, defaults to /certs")
	disableCertRotation                  = flag.Bool("disable-cert-rotation", false, "disable automatic generation and rotation of webhook TLS certificates/keys")
	enableProfile                        = flag.Bool("enable-pprof", false, "enable pprof profiling")
	profilePort                          = flag.Int("pprof-port", 6060, "port for pprof profiling. defaulted to 6060 if unspecified")
	certServiceName                      = flag.String("cert-service-name", "gatekeeper-webhook-service", "The service name used to generate the TLS cert's hostname. Defaults to gatekeeper-webhook-service")
	enableTLSHealthcheck                 = flag.Bool("enable-tls-healthcheck", false, "enable probing webhook API with certificate stored in certDir")
	disabledBuiltins                     = util.NewFlagSet()
	enableK8sCel                         = flag.Bool("enable-k8s-native-validation", true, "Beta: enable the validating admission policy driver")
	externaldataProviderResponseCacheTTL = flag.Duration("external-data-provider-response-cache-ttl", 3*time.Minute, "TTL for the external data provider response cache. Specify the duration in 'h', 'm', or 's' for hours, minutes, or seconds respectively. Defaults to 3 minutes if unspecified. Setting the TTL to 0 disables the cache.")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = api.AddToScheme(scheme)

	_ = configv1alpha1.AddToScheme(scheme)
	_ = statusv1beta1.AddToScheme(scheme)
	_ = mutationsv1alpha1.AddToScheme(scheme)
	_ = mutationsv1beta1.AddToScheme(scheme)
	_ = expansionv1alpha1.AddToScheme(scheme)
	_ = expansionv1beta1.AddToScheme(scheme)

	// +kubebuilder:scaffold:scheme
	flag.Var(disabledBuiltins, "disable-opa-builtin", "disable opa built-in function, this flag can be declared more than once.")
}

func main() {
	os.Exit(innerMain())
}

func innerMain() int {
	flag.Parse()
	encoder, ok := logLevelEncoders[*logLevelEncoder]
	if !ok {
		setupLog.Error(fmt.Errorf("invalid log level encoder: %v", *logLevelEncoder), "Invalid log level encoder")
		return 1
	}

	if *enableProfile {
		setupLog.Info(fmt.Sprintf("Starting profiling on port %d", *profilePort))
		go func() {
			addr := fmt.Sprintf("%s:%d", "localhost", *profilePort)
			server := http.Server{
				Addr:        addr,
				ReadTimeout: 5 * time.Second,
			}
			setupLog.Error(server.ListenAndServe(), "unable to start profiling server")
		}()
	}

	var logStream io.Writer
	if *logFile != "" {
		handle, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			setupLog.Error(fmt.Errorf("unable to open log file %s: %w", *logFile, err), "error initializing logging")
			return 1
		}
		defer handle.Close()
		logStream = handle
	}

	switch *logLevel {
	case "DEBUG":
		eCfg := zap.NewDevelopmentEncoderConfig()
		eCfg.LevelKey = *logLevelKey
		eCfg.EncodeLevel = encoder
		opts := []crzap.Opts{
			crzap.UseDevMode(true),
			crzap.Encoder(zapcore.NewConsoleEncoder(eCfg)),
		}
		if logStream != nil {
			opts = append(opts, crzap.WriteTo(logStream))
		}
		logger := crzap.New(opts...)
		ctrl.SetLogger(logger)
		klog.SetLogger(logger)
	case "WARNING", "ERROR":
		setLoggerForProduction(encoder, logStream)
	case "INFO":
		fallthrough
	default:
		eCfg := zap.NewProductionEncoderConfig()
		eCfg.LevelKey = *logLevelKey
		eCfg.EncodeLevel = encoder
		opts := []crzap.Opts{
			crzap.UseDevMode(false),
			crzap.Encoder(zapcore.NewJSONEncoder(eCfg)),
		}
		if logStream != nil {
			opts = append(opts, crzap.WriteTo(logStream))
		}
		logger := crzap.New(opts...)
		ctrl.SetLogger(logger)
		klog.SetLogger(logger)
	}

	if *mutation.DeprecatedMutationEnabled {
		setupLog.Error(errors.New("--enable-mutation flag is deprecated"), "use of deprecated flag")
	}

	config := ctrl.GetConfigOrDie()
	config.UserAgent = version.GetUserAgent("gatekeeper")
	setupLog.Info("setting up manager", "user agent", config.UserAgent)

	var webhooks []rotator.WebhookInfo
	webhooks = webhook.AppendValidationWebhookIfEnabled(webhooks)
	webhooks = webhook.AppendMutationWebhookIfEnabled(webhooks)

	// Disable high-cardinality REST client metrics (rest_client_request_latency).
	// Must be called before ctrl.NewManager!
	metrics.DisableRESTClientMetrics()

	tlsVersion, err := webhook.ParseTLSVersion(*webhook.TLSMinVersion)
	if err != nil {
		setupLog.Error(err, "unable to parse TLS version")
		return 1
	}
	serverOpts := crWebhook.Options{
		Host:    *host,
		Port:    *port,
		CertDir: *certDir,
		TLSOpts: []func(c *tls.Config){func(c *tls.Config) { c.MinVersion = tlsVersion }},
	}
	if *webhook.ClientCAName != "" {
		serverOpts.ClientCAName = *webhook.ClientCAName
		serverOpts.TLSOpts = []func(*tls.Config){
			func(cfg *tls.Config) {
				cfg.VerifyConnection = webhook.GetCertNameVerifier()
			},
		}
	}
	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: *metricsAddr,
		},
		LeaderElection:         false,
		WebhookServer:          crWebhook.NewServer(serverOpts),
		HealthProbeBindAddress: *healthAddr,
		MapperProvider:         apiutil.NewDynamicRESTMapper,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return 1
	}

	// Make sure certs are generated and valid if cert rotation is enabled.
	setupFinished := make(chan struct{})
	if !*disableCertRotation {
		setupLog.Info("setting up cert rotation")

		keyUsages := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		if *externaldata.ExternalDataEnabled {
			keyUsages = append(keyUsages, x509.ExtKeyUsageClientAuth)
		}

		if err := rotator.AddRotator(mgr, &rotator.CertRotator{
			SecretKey: types.NamespacedName{
				Namespace: util.GetNamespace(),
				Name:      secretName,
			},
			CertDir:        *certDir,
			CAName:         caName,
			CAOrganization: caOrganization,
			DNSName:        fmt.Sprintf("%s.%s.svc", *certServiceName, util.GetNamespace()),
			IsReady:        setupFinished,
			Webhooks:       webhooks,
			ExtKeyUsages:   &keyUsages,
		}); err != nil {
			setupLog.Error(err, "unable to set up cert rotation")
			return 1
		}
	} else {
		close(setupFinished)
	}

	// ControllerSwitch will be used to disable controllers during our teardown process,
	// avoiding conflicts in finalizer cleanup.
	sw := watch.NewSwitch()

	// Setup tracker and register readiness probe.
	tracker, err := readiness.SetupTracker(mgr, mutation.Enabled(), *externaldata.ExternalDataEnabled, *expansion.ExpansionEnabled)
	if err != nil {
		setupLog.Error(err, "unable to register readiness tracker")
		return 1
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("default", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to create health check")
		return 1
	}

	// only setup healthcheck when flag is set and available webhook count > 0
	if len(webhooks) > 0 && *enableTLSHealthcheck {
		tlsChecker := webhook.NewTLSChecker(*certDir, *port)
		setupLog.Info("setting up TLS healthcheck probe")
		if err := mgr.AddHealthzCheck("tls-check", tlsChecker); err != nil {
			setupLog.Error(err, "unable to create tls health check")
			return 1
		}
	}

	// Setup controllers asynchronously, they will block for certificate generation if needed.
	setupErr := make(chan error)
	ctx := ctrl.SetupSignalHandler()
	go func() {
		setupErr <- setupControllers(ctx, mgr, sw, tracker, setupFinished)
	}()

	setupLog.Info("starting manager")
	mgrErr := make(chan error)
	go func() {
		if err := mgr.Start(ctx); err != nil {
			setupLog.Error(err, "problem running manager")
			mgrErr <- err
		}
		close(mgrErr)
	}()

	// block until either setupControllers or mgr has an error, or mgr exits.
	// end after two events (one per goroutine) to guard against deadlock.
	hadError := false
blockingLoop:
	for i := 0; i < 2; i++ {
		select {
		case err := <-setupErr:
			if err != nil {
				hadError = true
				break blockingLoop
			}
		case err := <-mgrErr:
			if err != nil {
				hadError = true
			}
			// if manager has returned, we should exit the program
			break blockingLoop
		}
	}

	// Manager stops controllers asynchronously.
	// Instead, we use ControllerSwitch to synchronously prevent them from doing more work.
	// This can be removed when finalizer and status teardown is removed.
	setupLog.Info("disabling controllers...")
	sw.Stop()

	if hadError {
		return 1
	}
	return 0
}

func setupControllers(ctx context.Context, mgr ctrl.Manager, sw *watch.ControllerSwitch, tracker *readiness.Tracker, setupFinished chan struct{}) error {
	// Block until the setup (certificate generation) finishes.
	<-setupFinished

	var providerCache *frameworksexternaldata.ProviderCache
	args := []rego.Arg{rego.Tracing(false), rego.DisableBuiltins(disabledBuiltins.ToSlice()...)}
	mutationOpts := mutation.SystemOpts{Reporter: mutation.NewStatsReporter()}
	if *externaldata.ExternalDataEnabled {
		providerCache = frameworksexternaldata.NewCache()
		args = append(args, rego.AddExternalDataProviderCache(providerCache))
		mutationOpts.ProviderCache = providerCache

		switch {
		case *externaldataProviderResponseCacheTTL > 0:
			providerResponseCache := frameworksexternaldata.NewProviderResponseCache(ctx, *externaldataProviderResponseCacheTTL)
			args = append(args, rego.AddExternalDataProviderResponseCache(providerResponseCache))
		case *externaldataProviderResponseCacheTTL == 0:
			setupLog.Info("external data provider response cache is disabled")
		default:
			err := fmt.Errorf("invalid value for external-data-provider-response-cache-ttl: %d", *externaldataProviderResponseCacheTTL)
			setupLog.Error(err, "unable to create external data provider response cache")
			return err
		}

		certFile := filepath.Join(*certDir, certName)
		keyFile := filepath.Join(*certDir, keyName)

		// certWatcher is used to watch for changes to Gatekeeper's certificate and key files.
		certWatcher, err := certwatcher.New(certFile, keyFile)
		if err != nil {
			setupLog.Error(err, "unable to create client cert watcher")
			return err
		}

		setupLog.Info("setting up client cert watcher")
		if err := mgr.Add(certWatcher); err != nil {
			setupLog.Error(err, "unable to register client cert watcher")
			return err
		}

		// register the client cert watcher to the driver
		args = append(args, rego.EnableExternalDataClientAuth(), rego.AddExternalDataClientCertWatcher(certWatcher))

		// register the client cert watcher to the mutation system
		mutationOpts.ClientCertWatcher = certWatcher
	}

	cfArgs := []constraintclient.Opt{constraintclient.Targets(&target.K8sValidationTarget{})}

	if *enableK8sCel {
		k8sDriver, err := k8scel.New()
		if err != nil {
			setupLog.Error(err, "unable to set up K8s native driver")
			return err
		}
		cfArgs = append(cfArgs, constraintclient.Driver(k8sDriver))
	}

	driver, err := rego.New(args...)
	if err != nil {
		setupLog.Error(err, "unable to set up Driver")
		return err
	}
	cfArgs = append(cfArgs, constraintclient.Driver(driver))

	eps := []string{}
	if operations.IsAssigned(operations.Audit) {
		eps = append(eps, util.AuditEnforcementPoint)
	}
	if operations.IsAssigned(operations.Webhook) {
		eps = append(eps, util.WebhookEnforcementPoint)
	}

	cfArgs = append(cfArgs, constraintclient.EnforcementPoints(eps...))

	client, err := constraintclient.NewClient(cfArgs...)
	if err != nil {
		setupLog.Error(err, "unable to set up OPA client")
		return err
	}

	mutationSystem := mutation.NewSystem(mutationOpts)
	expansionSystem := expansion.NewSystem(mutationSystem)
	pubsubSystem := pubsub.NewSystem()

	c := mgr.GetCache()
	dc, ok := c.(watch.RemovableCache)
	if !ok {
		err := fmt.Errorf("expected dynamic cache, got: %T", c)
		setupLog.Error(err, "fetching dynamic cache")
		return err
	}

	setupLog.Info("setting up metrics")
	if err := metrics.AddToManager(mgr); err != nil {
		setupLog.Error(err, "unable to register metrics with the manager")
		return err
	}

	wm, err := watch.New(dc)
	if err != nil {
		setupLog.Error(err, "unable to create watch manager")
		return err
	}
	if err := mgr.Add(wm); err != nil {
		setupLog.Error(err, "unable to register watch manager with the manager")
		return err
	}

	// processExcluder is used for namespace exclusion for specified processes in config
	processExcluder := process.Get()

	// Setup all Controllers
	setupLog.Info("setting up controllers")

	// Events ch will be used to receive events from dynamic watches registered
	// via the registrar below.
	events := make(chan event.GenericEvent, 1024)
	reg, err := wm.NewRegistrar(
		cachemanager.RegistrarName,
		events)
	if err != nil {
		setupLog.Error(err, "unable to set up watch registrar for cache manager")
		return err
	}

	syncMetricsCache := syncutil.NewMetricsCache()
	cm, err := cachemanager.NewCacheManager(&cachemanager.Config{
		CfClient:         client,
		SyncMetricsCache: syncMetricsCache,
		Tracker:          tracker,
		ProcessExcluder:  processExcluder,
		Registrar:        reg,
		Reader:           mgr.GetCache(),
	})
	if err != nil {
		setupLog.Error(err, "unable to create cache manager")
		return err
	}

	err = mgr.Add(pruner.NewExpectationsPruner(cm, tracker))
	if err != nil {
		setupLog.Error(err, "adding expectations pruner to manager")
		return err
	}

	opts := controller.Dependencies{
		CFClient:         client,
		WatchManger:      wm,
		SyncEventsCh:     events,
		CacheMgr:         cm,
		ControllerSwitch: sw,
		Tracker:          tracker,
		ProcessExcluder:  processExcluder,
		MutationSystem:   mutationSystem,
		ExpansionSystem:  expansionSystem,
		ProviderCache:    providerCache,
		PubsubSystem:     pubsubSystem,
	}

	if err := controller.AddToManager(mgr, &opts); err != nil {
		setupLog.Error(err, "unable to register controllers with the manager")
		return err
	}

	if operations.IsAssigned(operations.Webhook) || operations.IsAssigned(operations.MutationWebhook) {
		setupLog.Info("setting up webhooks")
		webhookDeps := webhook.Dependencies{
			OpaClient:       client,
			ProcessExcluder: processExcluder,
			MutationSystem:  mutationSystem,
			ExpansionSystem: expansionSystem,
		}
		if err := webhook.AddToManager(mgr, webhookDeps); err != nil {
			setupLog.Error(err, "unable to register webhooks with the manager")
			return err
		}
	}

	if operations.IsAssigned(operations.Audit) {
		setupLog.Info("setting up audit")
		auditCache := audit.NewAuditCacheLister(mgr.GetCache(), cm)
		auditDeps := audit.Dependencies{
			Client:          client,
			ProcessExcluder: processExcluder,
			CacheLister:     auditCache,
			ExpansionSystem: expansionSystem,
			PubSubSystem:    pubsubSystem,
		}
		if err := audit.AddToManager(mgr, &auditDeps); err != nil {
			setupLog.Error(err, "unable to register audit with the manager")
			return err
		}
	}

	setupLog.Info("setting up upgrade")
	if err := upgrade.AddToManager(mgr); err != nil {
		setupLog.Error(err, "unable to register upgrade with the manager")
		return err
	}

	return nil
}

func setLoggerForProduction(encoder zapcore.LevelEncoder, dest io.Writer) {
	sink := zapcore.AddSync(os.Stderr)
	if dest != nil {
		sink = zapcore.AddSync(dest)
	}
	var opts []zap.Option
	encCfg := zap.NewProductionEncoderConfig()
	encCfg.LevelKey = *logLevelKey
	encCfg.EncodeLevel = encoder
	enc := zapcore.NewJSONEncoder(encCfg)
	lvl := zap.NewAtomicLevelAt(zap.WarnLevel)
	opts = append(opts, zap.AddStacktrace(zap.ErrorLevel),
		zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewSamplerWithOptions(core, time.Second, 100, 100)
		}),
		zap.AddCallerSkip(1), zap.ErrorOutput(sink))
	zlog := zap.New(zapcore.NewCore(&crzap.KubeAwareEncoder{Encoder: enc, Verbose: false}, sink, lvl))
	zlog = zlog.WithOptions(opts...)
	newlogger := zapr.NewLogger(zlog)
	ctrl.SetLogger(newlogger)
	klog.SetLogger(newlogger)
}
