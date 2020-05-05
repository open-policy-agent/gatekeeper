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
	"os"
	"time"

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
	certName               = "tls.crt"
	keyName                = "tls.key"
	caCertName             = "ca.crt"
	caKeyName              = "ca.key"
	rotationCheckFrequency = 12 * time.Hour
	certValidityDuration   = 10 * 365 * 24 * time.Hour
)

var crLog = logf.Log.WithName("cert-rotation")

var vwhGVK = schema.GroupVersionKind{Group: "admissionregistration.k8s.io", Version: "v1beta1", Kind: "ValidatingWebhookConfiguration"}

var _ manager.Runnable = &CertRotator{}

// AddRotator adds the CertRotator and ReconcileVWH to the manager.
func AddRotator(mgr manager.Manager, cr *CertRotator, vwhName string) error {
	cr.client = mgr.GetClient()
	cr.certsNotMounted = make(chan struct{})
	if err := mgr.Add(cr); err != nil {
		return err
	}

	vwhKey := types.NamespacedName{Name: vwhName}
	reconciler := &ReconcileVWH{
		client:    mgr.GetClient(),
		scheme:    mgr.GetScheme(),
		ctx:       context.Background(),
		secretKey: cr.SecretKey,
		vwhKey:    vwhKey,
	}
	if err := addController(mgr, reconciler, cr.SecretKey, vwhKey); err != nil {
		return err
	}
	return nil
}

// CertRotator contains cert artifacts and a channel to close when the certs are ready.
type CertRotator struct {
	client          client.Client
	SecretKey       types.NamespacedName
	CertDir         string
	CAName          string
	CAOrganization  string
	DNSName         string
	CertsMounted    chan struct{}
	certsNotMounted chan struct{}
}

// Start starts the CertRotator runnable to rotate certs and ensure the certs are ready.
func (cr *CertRotator) Start(stop <-chan (struct{})) error {
	// explicitly rotate on the first round so that the certificate
	// can be bootstrapped, otherwise manager exits before a cert can be written
	crLog.Info("starting cert rotator controller")
	defer crLog.Info("stopping cert rotator controller")
	if err := cr.refreshCertIfNeeded(); err != nil {
		crLog.Error(err, "could not refresh cert on startup")
		return err
	}

	// Once the certs are ready, close the channel.
	go cr.ensureCertsExist()

	ticker := time.NewTicker(rotationCheckFrequency)

tickerLoop:
	for {
		select {
		case <-ticker.C:
			if err := cr.refreshCertIfNeeded(); err != nil {
				crLog.Error(err, "error rotating certs")
			}
		case <-stop:
			break tickerLoop
		case <-cr.certsNotMounted:
			return errors.New("could not mount certs")
		}
	}

	ticker.Stop()
	return nil
}

// refreshCertIfNeeded returns whether there's any error when refreshing the certs if needed.
func (cr *CertRotator) refreshCertIfNeeded() error {
	refreshFn := func() (bool, error) {
		secret := &corev1.Secret{}
		if err := cr.client.Get(context.Background(), cr.SecretKey, secret); err != nil {
			return false, errors.Wrap(err, "acquiring secret to update certificates")
		}
		if secret.Data == nil || !cr.validCACert(secret.Data[caCertName], secret.Data[caKeyName]) {
			crLog.Info("refreshing CA and server certs")
			if err := cr.refreshCerts(true, secret); err != nil {
				crLog.Error(err, "could not refresh CA and server certs")
				return false, nil
			}
			crLog.Info("server certs refreshed")
			return true, nil
		}
		// make sure our reconciler is initialized on startup (either this or the above refreshCerts() will call this)
		if !cr.validServerCert(secret.Data[caCertName], secret.Data[certName], secret.Data[keyName]) {
			crLog.Info("refreshing server certs")
			if err := cr.refreshCerts(false, secret); err != nil {
				crLog.Error(err, "could not refresh server certs")
				return false, nil
			}
			crLog.Info("server certs refreshed")
			return true, nil
		}
		crLog.Info("no cert refresh needed")
		return true, nil
	}
	if err := wait.ExponentialBackoff(wait.Backoff{
		Duration: 10 * time.Millisecond,
		Factor:   2,
		Jitter:   1,
		Steps:    10,
	}, refreshFn); err != nil {
		return err
	}
	return nil
}

func (cr *CertRotator) refreshCerts(refreshCA bool, secret *corev1.Secret) error {
	var caArtifacts *KeyPairArtifacts
	if refreshCA {
		var err error
		caArtifacts, err = cr.createCACert()
		if err != nil {
			return err
		}
	} else {
		var err error
		caArtifacts, err = buildArtifactsFromSecret(secret)
		if err != nil {
			return err
		}
	}
	cert, key, err := cr.createCertPEM(caArtifacts)
	if err != nil {
		return err
	}
	if err := cr.writeSecret(cert, key, caArtifacts, secret); err != nil {
		return err
	}
	return nil
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

func (cr *CertRotator) writeSecret(cert, key []byte, caArtifacts *KeyPairArtifacts, secret *corev1.Secret) error {
	populateSecret(cert, key, caArtifacts, secret)
	return cr.client.Update(context.Background(), secret)
}

// KeyPairArtifacts stores cert artifacts.
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
func (cr *CertRotator) createCACert() (*KeyPairArtifacts, error) {
	now := time.Now()
	begin := now.Add(-1 * time.Hour)
	end := now.Add(certValidityDuration)
	templ := &x509.Certificate{
		SerialNumber: big.NewInt(0),
		Subject: pkix.Name{
			CommonName:   cr.CAName,
			Organization: []string{cr.CAOrganization},
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
func (cr *CertRotator) createCertPEM(ca *KeyPairArtifacts) ([]byte, []byte, error) {
	now := time.Now()
	begin := now.Add(-1 * time.Hour)
	end := now.Add(certValidityDuration)
	templ := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: cr.DNSName,
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

func (cr *CertRotator) validServerCert(caCert, cert, key []byte) bool {
	valid, err := validCert(caCert, cert, key, cr.DNSName, lookaheadTime())
	if err != nil {
		return false
	}
	return valid
}

func (cr *CertRotator) validCACert(cert, key []byte) bool {
	valid, err := validCert(cert, cert, key, cr.CAName, lookaheadTime())
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

type mapper struct {
	secretKey types.NamespacedName
	vwhKey    types.NamespacedName
}

func (m *mapper) Map(object handler.MapObject) []reconcile.Request {
	if object.Meta.GetNamespace() != m.vwhKey.Namespace {
		return nil
	}
	if object.Meta.GetName() != m.vwhKey.Name {
		return nil
	}
	return []reconcile.Request{{NamespacedName: m.secretKey}}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func addController(mgr manager.Manager, r reconcile.Reconciler, secretKey, vwhKey types.NamespacedName) error {
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
	mapper := &handler.EnqueueRequestsFromMapFunc{ToRequests: &mapper{
		secretKey: secretKey,
		vwhKey:    vwhKey,
	}}
	if err := c.Watch(&source.Kind{Type: vwh}, mapper); err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileVWH{}

// ReconcileVWH reconciles a validatingwebhookconfiguration, making sure it
// has the appropriate CA cert
type ReconcileVWH struct {
	client    client.Client
	scheme    *runtime.Scheme
	ctx       context.Context
	secretKey types.NamespacedName
	vwhKey    types.NamespacedName
}

// Reconcile reads that state of the cluster for a validatingwebhookconfiguration
// object and makes sure the most recent CA cert is included
func (r *ReconcileVWH) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	if request.NamespacedName != r.secretKey {
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
	if err := r.client.Get(r.ctx, r.vwhKey, vwh); err != nil {
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
			crLog.Error(err, "secret is not well-formed, cannot update ValidatingWebhookConfiguration")
			return reconcile.Result{}, nil
		}
		crLog.Info("ensuring CA cert on ValidatingWebhookConfiguration")
		if err = injectCertToWebhook(vwh, artifacts.CertPEM); err != nil {
			crLog.Error(err, "unable to inject cert to webhook")
			return reconcile.Result{}, err
		}
		if err := r.client.Update(r.ctx, vwh); err != nil {
			return reconcile.Result{Requeue: true}, err
		}
	}

	return reconcile.Result{}, nil
}

// ensureCertsExist ensure the cert files exist.
func (cr *CertRotator) ensureCertsExist() {
	checkFn := func() (bool, error) {
		certFile := cr.CertDir + "/" + certName
		_, err := os.Stat(certFile)
		if err == nil {
			return true, nil
		}
		return false, nil
	}
	if err := wait.ExponentialBackoff(wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   2,
		Jitter:   1,
		Steps:    10,
	}, checkFn); err != nil {
		crLog.Error(err, "max retries for checking certs existence")
		close(cr.certsNotMounted)
		return
	}
	crLog.Info(fmt.Sprintf("certs are ready in %s", cr.CertDir))
	close(cr.CertsMounted)
}
