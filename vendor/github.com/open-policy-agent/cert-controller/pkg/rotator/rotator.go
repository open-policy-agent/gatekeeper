package rotator

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
	"flag"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

var crdGVK = schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1beta1", Kind: "CustomResourceDefinition"}

var _ manager.Runnable = &CertRotator{}

var restartOnSecretRefresh = false

func init() {
	flag.BoolVar(&restartOnSecretRefresh, "cert-restart-on-secret-refresh", false, "Kills the process when secrets are refreshed so that the pod can be restarted (secrets take up to 60s to be updated by running pods)")
}

// AddRotator adds the CertRotator and ReconcileWH to the manager.
func AddRotator(mgr manager.Manager, cr *CertRotator) error {
	cr.client = mgr.GetClient()
	cr.certsMounted = make(chan struct{})
	cr.certsNotMounted = make(chan struct{})
	cr.wasCAInjected = atomic.NewBool(false)
	cr.caNotInjected = make(chan struct{})
	if err := mgr.Add(cr); err != nil {
		return err
	}

	vwhKey := types.NamespacedName{Name: cr.VWHName}
	reconciler := &ReconcileWH{
		client:        mgr.GetClient(),
		scheme:        mgr.GetScheme(),
		ctx:           context.Background(),
		secretKey:     cr.SecretKey,
		vwhKey:        vwhKey,
		crdNames:      cr.CRDNames,
		wasCAInjected: cr.wasCAInjected,
	}
	if err := addController(mgr, reconciler); err != nil {
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
	IsReady         chan struct{}
	certsMounted    chan struct{}
	certsNotMounted chan struct{}
	wasCAInjected   *atomic.Bool
	caNotInjected   chan struct{}
	VWHName         string
	CRDNames        []string
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
	go cr.ensureCertsMounted()
	go cr.ensureReady()

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
		case <-cr.caNotInjected:
			return errors.New("could not inject certs to webhooks")
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
			if restartOnSecretRefresh {
				crLog.Info("Secrets have been updated; exiting so pod can be restarted (omit --cert-restart-on-secret-refresh to wait instead of restarting")
				os.Exit(0)
			}
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
			if restartOnSecretRefresh {
				crLog.Info("Secrets have been updated; exiting so pod can be restarted (omit --cert-restart-on-secret-refresh to wait instead of restarting")
				os.Exit(0)
			}
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

func injectCertToConversionWebhook(crd *unstructured.Unstructured, certPem []byte) error {
	_, found, err := unstructured.NestedMap(crd.Object, "spec", "conversion", "webhookClientConfig")
	if err != nil {
		return err
	}
	if !found {
		return errors.New("`webhookClientConfig` field not found in CustomResourceDefinition")
	}
	if err := unstructured.SetNestedField(crd.Object, base64.StdEncoding.EncodeToString(certPem), "spec", "conversion", "webhookClientConfig", "caBundle"); err != nil {
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
		DNSNames: []string{
			cr.CAName,
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
		DNSNames: []string{
			cr.DNSName,
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
// webhooks don't get clobbered

var _ handler.Mapper = &crdMapper{}

type crdMapper struct {
	secretKey types.NamespacedName
	crdNames  []string
}

func (m *crdMapper) Map(object handler.MapObject) []reconcile.Request {
	if object.Meta.GetNamespace() != "" {
		return nil
	}
	for _, crdName := range m.crdNames {
		if object.Meta.GetName() == crdName {
			return []reconcile.Request{{NamespacedName: m.secretKey}}
		}
	}
	return nil
}

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
func addController(mgr manager.Manager, r *ReconcileWH) error {
	vwh := &unstructured.Unstructured{}
	vwh.SetGroupVersionKind(vwhGVK)
	crd := &unstructured.Unstructured{}
	crd.SetGroupVersionKind(crdGVK)
	// Create a new controller
	err := ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		Watches(&source.Kind{Type: vwh}, &handler.EnqueueRequestsFromMapFunc{ToRequests: &mapper{
			secretKey: r.secretKey,
			vwhKey:    r.vwhKey,
		}}).
		Watches(&source.Kind{Type: crd}, &handler.EnqueueRequestsFromMapFunc{ToRequests: &crdMapper{
			secretKey: r.secretKey,
			crdNames:  r.crdNames,
		}}).Complete(r)
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileWH{}

// ReconcileWH reconciles a validatingwebhookconfiguration, making sure it
// has the appropriate CA cert
type ReconcileWH struct {
	client        client.Client
	scheme        *runtime.Scheme
	ctx           context.Context
	secretKey     types.NamespacedName
	vwhKey        types.NamespacedName
	crdNames      []string
	wasCAInjected *atomic.Bool
}

// Reconcile reads that state of the cluster for a validatingwebhookconfiguration
// object and makes sure the most recent CA cert is included
func (r *ReconcileWH) Reconcile(request reconcile.Request) (reconcile.Result, error) {
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

	if secret.GetDeletionTimestamp().IsZero() {
		artifacts, err := buildArtifactsFromSecret(secret)
		if err != nil {
			crLog.Error(err, "secret is not well-formed, cannot update webhook configurations")
			return reconcile.Result{}, nil
		}

		// Ensure certs on validating webhooks.
		errVWH := r.ensureVWHCerts(artifacts.CertPEM)

		// Ensure certs on CRD conversion webhooks if there's any.
		errCWH := r.ensureCRDConvWHCerts(artifacts.CertPEM)

		// Return errors if there's any when trying to inject certs to all webhooks.
		if errVWH != nil {
			return reconcile.Result{}, errVWH
		}
		if errCWH != nil {
			return reconcile.Result{}, errCWH
		}

		// Set CAInjected if the reconciler has not exited early.
		r.wasCAInjected.Store(true)
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileWH) ensureVWHCerts(certPem []byte) error {
	vwh := &unstructured.Unstructured{}
	vwh.SetGroupVersionKind(vwhGVK)
	if err := r.client.Get(r.ctx, r.vwhKey, vwh); err != nil {
		if k8sErrors.IsNotFound(err) {
			crLog.Info("VWK " + r.vwhKey.Name + " is not found. No action is needed.")
			return nil
		}
		// Error reading the object - requeue the request.
		return err
	}

	crLog.Info("ensuring CA cert on ValidatingWebhookConfiguration")
	if err := injectCertToWebhook(vwh, certPem); err != nil {
		crLog.Error(err, "unable to inject cert to webhook")
		return err
	}
	if err := r.client.Update(r.ctx, vwh); err != nil {
		return err
	}

	return nil
}

// ensureCRDConvWHCerts returns an arbitrary error if multiple errors are
// encountered, while all the errors are logged.
func (r *ReconcileWH) ensureCRDConvWHCerts(certPem []byte) error {
	var anyError error = nil
	for _, crdName := range r.crdNames {
		crd := &unstructured.Unstructured{}
		crd.SetGroupVersionKind(crdGVK)
		crdKey := types.NamespacedName{Name: crdName}
		if err := r.client.Get(r.ctx, crdKey, crd); err != nil {
			if k8sErrors.IsNotFound(err) {
				crLog.Info("CRD " + crdName + " is not found")
				continue
			}
			// Error reading the object - requeue the request.
			crLog.Error(err, "unable to get CRD")
			anyError = err
			continue
		}

		if !crd.GetDeletionTimestamp().IsZero() {
			crLog.Info("CRD " + crdName + " is being deleted")
			continue
		}

		crLog.Info("ensuring CA cert on CRD conversion webhook")
		if err := injectCertToConversionWebhook(crd, certPem); err != nil {
			crLog.Error(err, "unable to inject cert to CRD conversion webhook")
			anyError = err
			continue
		}
		if err := r.client.Update(r.ctx, crd); err != nil {
			crLog.Error(err, "unable to update cert on CRD "+crdName)
			anyError = err
		}
	}

	return anyError
}

// ensureCertsMounted ensure the cert files exist.
func (cr *CertRotator) ensureCertsMounted() {
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
	close(cr.certsMounted)
}

// ensureReady ensure the cert files exist and the CAs are injected.
func (cr *CertRotator) ensureReady() {
	<-cr.certsMounted
	checkFn := func() (bool, error) {
		return cr.wasCAInjected.Load(), nil
	}
	if err := wait.ExponentialBackoff(wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   2,
		Jitter:   1,
		Steps:    10,
	}, checkFn); err != nil {
		crLog.Error(err, "max retries for checking CA injection")
		close(cr.caNotInjected)
		return
	}
	crLog.Info(fmt.Sprintf("CA certs are injected to webhooks"))
	close(cr.IsReady)
}
