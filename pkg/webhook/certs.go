package webhook

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	caName                 = "gatekeeper-ca"
	service                = "gatekeeper-webhook-service"
	certName               = "tls.crt"
	keyName                = "tls.key"
	caCertName             = "ca.crt"
	caKeyName              = "ca.key"
	rotationCheckFrequency = 12 * time.Hour
	certValidityDuration   = 10 * 365 * 24 * time.Hour
)

var crLog = logf.Log.WithName("cert-rotation")

var (
	secretKey = types.NamespacedName{
		Namespace: util.GetNamespace(),
		Name:      "gatekeeper-webhook-server-cert",
	}
	// DNSName is <service name>.<namespace>.svc
	DNSName = fmt.Sprintf("%s.%s.svc", service, util.GetNamespace())
	vwhGVK  = schema.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1beta1", Kind: "ValidatingWebhookConfiguration"}
	vwhKey  = types.NamespacedName{Name: "gatekeeper-validating-webhook-configuration"}
)

var _ manager.Runnable = &certRotator{}

func AddRotator(mgr manager.Manager) error {
	// Use a new client so we are unaffected by the cache sync kill signal
	cli, err := client.New(mgr.GetConfig(), client.Options{Scheme: mgr.GetScheme(), Mapper: mgr.GetRESTMapper()})
	if err != nil {
		return err
	}
	rotator := &certRotator{client: cli}
	if err = mgr.Add(rotator); err != nil {
		return err
	}

	reconciler := &ReconcileVWH{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
		ctx:    context.Background(),
	}
	if err := addController(mgr, reconciler); err != nil {
		return err
	}
	return nil
}

type certRotator struct {
	client client.Client
}

func (cr *certRotator) Start(stop <-chan (struct{})) error {
	// explicitly rotate on the first round so that the certificate
	// can be bootstrapped, otherwise manager exits before a cert can be written
	crLog.Info("starting cert rotator controller")
	defer crLog.Info("stopping cert rotator controller")
	if restart, err := cr.refreshCertIfNeeded(); err != nil {
		crLog.Error(err, "could not refresh cert on startup")
		return errors.Wrap(err, "could not refresh cert on startup")
	} else if restart {
		crLog.Info("certs refreshed, restarting server")
		return nil
	}
	ticker := time.NewTicker(rotationCheckFrequency)

tickerLoop:
	for {
		select {
		case <-ticker.C:
			if restart, err := cr.refreshCertIfNeeded(); err != nil {
				crLog.Error(err, "error rotating certs")
			} else if restart {
				crLog.Info("certs refreshed, restarting server")
				break tickerLoop
			}
		case <-stop:
			break tickerLoop
		}
	}

	ticker.Stop()
	return nil
}

// refreshCertIfNeeded returns whether the cert was refreshed and any errors
func (cr *certRotator) refreshCertIfNeeded() (bool, error) {
	secret := &corev1.Secret{}
	if err := cr.client.Get(context.Background(), secretKey, secret); err != nil {
		return false, errors.Wrap(err, "acquiring secret to update certificates")
	}
	if secret.Data == nil || !validCACert(secret.Data[caCertName], secret.Data[caKeyName]) {
		crLog.Info("refreshing CA and server certs")
		return cr.refreshCerts(true, secret)
	}
	// make sure our reconciler is initialized on startup (either this or the above refreshCerts() will call this)
	if !validServerCert(secret.Data[caCertName], secret.Data[certName], secret.Data[keyName]) {
		crLog.Info("refreshing server certs")
		return cr.refreshCerts(false, secret)
	}
	crLog.Info("no cert refresh needed")
	return false, nil
}

func (cr *certRotator) refreshCerts(refreshCA bool, secret *corev1.Secret) (bool, error) {
	var caArtifacts *KeyPairArtifacts
	if refreshCA {
		var err error
		caArtifacts, err = createCACert()
		if err != nil {
			return false, err
		}
	} else {
		var err error
		caArtifacts, err = buildArtifactsFromSecret(secret)
		if err != nil {
			return false, err
		}
	}
	cert, key, err := createCertPEM(caArtifacts)
	if err != nil {
		return false, err
	}
	writeFn := func() (bool, error) {
		if err := cr.writeSecret(cert, key, caArtifacts, secret); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := wait.ExponentialBackoff(wait.Backoff{
		Duration: 10 * time.Millisecond,
		Factor:   2,
		Jitter:   1,
		Steps:    10,
	}, writeFn); err != nil {
		log.Error(err, "max retries for writing of secret reached")
		return false, err
	}
	return true, nil
}

func injectCertToWebhook(vwh *unstructured.Unstructured, certPem []byte) error {
	webhooks, found, err := unstructured.NestedSlice(vwh.Object, "webhooks")
	if err != nil {
		return err
	}
	if !found {
		return errors.New("`webhooks` field not found in ValidatingWebhookConfiguration")
	}
	for i, h := range webhooks {
		hook, ok := h.(map[string]interface{})
		if !ok {
			return errors.Errorf("webhook %d is not well-formed", i)
		}
		if err := unstructured.SetNestedField(hook, base64.StdEncoding.EncodeToString(certPem), "clientConfig", "caBundle"); err != nil {
			return err
		}
		webhooks[i] = hook
	}
	if err := unstructured.SetNestedSlice(vwh.Object, webhooks, "webhooks"); err != nil {
		return err
	}
	return nil
}

func (cr *certRotator) writeSecret(cert, key []byte, caArtifacts *KeyPairArtifacts, secret *corev1.Secret) error {
	populateSecret(cert, key, caArtifacts, secret)
	return cr.client.Update(context.Background(), secret)
}

type KeyPairArtifacts struct {
	Cert    *x509.Certificate
	Key     *rsa.PrivateKey
	CertPEM []byte
	KeyPEM  []byte
}

func populateSecret(cert, key []byte, caArtifacts *KeyPairArtifacts, secret *corev1.Secret) {
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data[caCertName] = caArtifacts.CertPEM
	secret.Data[caKeyName] = caArtifacts.KeyPEM
	secret.Data[certName] = cert
	secret.Data[keyName] = key
}

func buildArtifactsFromSecret(secret *corev1.Secret) (*KeyPairArtifacts, error) {
	caPem, ok := secret.Data[caCertName]
	if !ok {
		return nil, errors.New(fmt.Sprintf("Cert secret is not well-formed, missing %s", caCertName))
	}
	keyPem, ok := secret.Data[caKeyName]
	if !ok {
		return nil, errors.New(fmt.Sprintf("Cert secret is not well-formed, missing %s", caKeyName))
	}
	caDer, _ := pem.Decode(caPem)
	if caDer == nil {
		return nil, errors.New("bad CA cert")
	}
	caCert, err := x509.ParseCertificate(caDer.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "while parsing CA cert")
	}
	keyDer, _ := pem.Decode(keyPem)
	if keyDer == nil {
		return nil, errors.New("bad CA cert")
	}
	key, err := x509.ParsePKCS1PrivateKey(keyDer.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "while parsing CA key")
	}
	return &KeyPairArtifacts{
		Cert:    caCert,
		CertPEM: caPem,
		KeyPEM:  keyPem,
		Key:     key,
	}, nil
}

// createCACert creates the self-signed CA cert and private key that will
// be used to sign the server certificate
func createCACert() (*KeyPairArtifacts, error) {
	now := time.Now()
	begin := now.Add(-1 * time.Hour)
	end := now.Add(certValidityDuration)
	templ := &x509.Certificate{
		SerialNumber: big.NewInt(0),
		Subject: pkix.Name{
			CommonName:   caName,
			Organization: []string{"gatekeeper"},
		},
		NotBefore:             begin,
		NotAfter:              end,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, errors.Wrap(err, "generating key")
	}
	der, err := x509.CreateCertificate(rand.Reader, templ, templ, key.Public(), key)
	if err != nil {
		return nil, errors.Wrap(err, "creating certificate")
	}
	certPEM, keyPEM, err := pemEncode(der, key)
	if err != nil {
		return nil, errors.Wrap(err, "encoding PEM")
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, errors.Wrap(err, "parsing certificate")
	}

	return &KeyPairArtifacts{Cert: cert, Key: key, CertPEM: certPEM, KeyPEM: keyPEM}, nil
}

// createCertPEM takes the results of createCACert and uses it to create the
// PEM-encoded public certificate and private key, respectively
func createCertPEM(ca *KeyPairArtifacts) ([]byte, []byte, error) {
	now := time.Now()
	begin := now.Add(-1 * time.Hour)
	end := now.Add(certValidityDuration)
	templ := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: DNSName,
		},
		NotBefore:             begin,
		NotAfter:              end,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, errors.Wrap(err, "generating key")
	}
	der, err := x509.CreateCertificate(rand.Reader, templ, ca.Cert, key.Public(), ca.Key)
	if err != nil {
		return nil, nil, errors.Wrap(err, "creating certificate")
	}
	certPEM, keyPEM, err := pemEncode(der, key)
	if err != nil {
		return nil, nil, errors.Wrap(err, "encoding PEM")
	}
	return certPEM, keyPEM, nil
}

// pemEncode takes a certificate and encodes it as PEM
func pemEncode(certificateDER []byte, key *rsa.PrivateKey) ([]byte, []byte, error) {
	certBuf := &bytes.Buffer{}
	if err := pem.Encode(certBuf, &pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}); err != nil {
		return nil, nil, errors.Wrap(err, "encoding cert")
	}
	keyBuf := &bytes.Buffer{}
	if err := pem.Encode(keyBuf, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		return nil, nil, errors.Wrap(err, "encoding key")
	}
	return certBuf.Bytes(), keyBuf.Bytes(), nil
}

func lookaheadTime() time.Time {
	return time.Now().Add(90 * 24 * time.Hour)
}

func validServerCert(caCert, cert, key []byte) bool {
	valid, err := validCert(caCert, cert, key, DNSName, lookaheadTime())
	if err != nil {
		return false
	}
	return valid
}

func validCACert(cert, key []byte) bool {
	valid, err := validCert(cert, cert, key, caName, lookaheadTime())
	if err != nil {
		return false
	}
	return valid
}

func validCert(caCert, cert, key []byte, dnsName string, at time.Time) (bool, error) {
	if len(caCert) == 0 || len(cert) == 0 || len(key) == 0 {
		return false, errors.New("empty cert")
	}

	pool := x509.NewCertPool()
	caDer, _ := pem.Decode(caCert)
	if caDer == nil {
		return false, errors.New("bad CA cert")
	}
	cac, err := x509.ParseCertificate(caDer.Bytes)
	if err != nil {
		return false, errors.Wrap(err, "parsing CA cert")
	}
	pool.AddCert(cac)

	_, err = tls.X509KeyPair(cert, key)
	if err != nil {
		return false, errors.Wrap(err, "building key pair")
	}

	b, _ := pem.Decode(cert)
	if b == nil {
		return false, errors.New("bad private key")
	}

	crt, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		return false, errors.Wrap(err, "parsing cert")
	}
	_, err = crt.Verify(x509.VerifyOptions{
		DNSName:     dnsName,
		Roots:       pool,
		CurrentTime: at,
	})
	if err != nil {
		return false, errors.Wrap(err, "verifying cert")
	}
	return true, nil
}

// controller code for making sure the CA cert on the
// validatingwebhookconfiguration doesn't get clobbered

var _ handler.Mapper = &mapper{}

type mapper struct{}

func (m *mapper) Map(object handler.MapObject) []reconcile.Request {
	if object.Meta.GetNamespace() != vwhKey.Namespace {
		return nil
	}
	if object.Meta.GetName() != vwhKey.Name {
		return nil
	}
	return []reconcile.Request{{NamespacedName: secretKey}}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func addController(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New(("validating-webhook-controller"), mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to the provided ValidatingWebhookConfiguration
	s := &corev1.Secret{}
	if err := c.Watch(&source.Kind{Type: s}, &handler.EnqueueRequestForObject{}); err != nil {
		return err
	}

	vwh := &unstructured.Unstructured{}
	vwh.SetGroupVersionKind(vwhGVK)
	mapper := &handler.EnqueueRequestsFromMapFunc{ToRequests: &mapper{}}
	if err := c.Watch(&source.Kind{Type: vwh}, mapper); err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileVWH{}

// ReconcileVWH reconciles a validatingwebhookconfiguration, making sure it
// has the appropriate CA cert
type ReconcileVWH struct {
	client client.Client
	scheme *runtime.Scheme
	ctx    context.Context
}

// Reconcile reads that state of the cluster for a validatingwebhookconfiguration
// object and makes sure the most recent CA cert is included
func (r *ReconcileVWH) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	if request.NamespacedName != secretKey {
		return reconcile.Result{}, nil
	}
	secret := &corev1.Secret{}
	if err := r.client.Get(r.ctx, request.NamespacedName, secret); err != nil {
		if k8sErrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{Requeue: true}, err
	}

	vwh := &unstructured.Unstructured{}
	vwh.SetGroupVersionKind(vwhGVK)
	if err := r.client.Get(r.ctx, vwhKey, vwh); err != nil {
		if k8sErrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{Requeue: true}, err
	}

	if secret.GetDeletionTimestamp().IsZero() {
		artifacts, err := buildArtifactsFromSecret(secret)
		if err != nil {
			log.Error(err, "secret is not well-formed, cannot update ValidatingWebhookConfiguration")
			return reconcile.Result{}, nil
		}
		log.Info("ensuring CA cert on ValidatingWebhookConfiguration")
		if err = injectCertToWebhook(vwh, artifacts.CertPEM); err != nil {
			log.Error(err, "unable to inject cert to webhook")
			return reconcile.Result{}, err
		}
		if err := r.client.Update(r.ctx, vwh); err != nil {
			return reconcile.Result{Requeue: true}, err
		}
	}

	return reconcile.Result{}, nil
}
