package webhook

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"github.com/pkg/errors"
	"k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"math/big"
	"time"
)

const (
	DNSName = "gatekeeper-controller-manager-service.gatekeeper-system.svc"
	CAName = "gatekeeper-ca"
	namespace = "gatekeeper-system"
	certName = "tls.crt"
	keyName = "tls.key"
	caCertName = "ca.crt"
	caKeyName = "ca.key"
)

var crLog = logf.Log.WithName("cert-rotation")

var (
	secretKey = types.NamespacedName{
		Namespace: namespace,
		Name: "gatekeeper-webhook-server-secret",
	}
)

var _ manager.Runnable = &certRotator{}

type certRotator struct {
	client client.Client
}

func (cr *certRotator) Start(stop <-chan(struct{})) error {
	ticker := time.NewTicker(10 * time.Second)
	done := make(chan struct{})
	go func() {
		for {
			select {
				case <-ticker.C:
					if err := cr.refreshCertIfNeeded(); err != nil {
						crLog.Error(err, "error rotating certs")
					}
				case <-stop:
					close(done)
					return
			}
		}
	}()
	
	<-done
	ticker.Stop()
	return nil
}

func (cr *certRotator) refreshCertIfNeeded() error {
	secret := &corev1.Secret{}
	if err := cr.client.Get(context.Background(), secretKey, secret); err != nil {
		return errors.New("Unable to acquire secret to update certificates")
	}
	if secret.Data == nil || !validCACert(secret.Data[caCertName], secret.Data[caKeyName]) {
		return cr.refreshCerts(true, secret)
	}
	if !validServerCert(secret.Data[caCertName], secret.Data[certName], secret.Data[keyName]) {
		return cr.refreshCerts(false, secret)
	}
	return nil
}

func (cr *certRotator) refreshCerts(refreshCA bool, secret *corev1.Secret) error {
	var caArtifacts *KeyPairArtifacts
	if refreshCA {
		var err error
		caArtifacts, err = createCACert()
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
	cert, key, err := createCertPEM(caArtifacts)
	if err != nil {
		return err
	}
	// execute writeWebhookConfig first because update is triggered when the
	// secret is invalid. Therefore writing the secret last means that this
	// process will be re-triggered if any update fails.
	if err := cr.writeWebhookConfig(caArtifacts.CertPEM); err != nil {
		return err
	}
	return cr.writeSecret(cert, key, caArtifacts, secret)
}

func (cr *certRotator) writeWebhookConfig(certPem []byte) error {
	vwh := &v1beta1.ValidatingWebhookConfiguration{}
	vwhKey := types.NamespacedName{Name: *webhookName}
	if err := cr.client.Get(context.Background(), vwhKey, vwh); err != nil {
		return err
	}
	for i := range vwh.Webhooks {
		vwh.Webhooks[i].ClientConfig.CABundle = certPem
	}
	return cr.client.Update(context.Background(), vwh)
}

func (cr *certRotator) writeSecret(cert, key []byte, caArtifacts *KeyPairArtifacts, secret *corev1.Secret) error {
	populateSecret(cert, key, caArtifacts, secret)
	return cr.client.Update(context.Background(), secret)
}

type KeyPairArtifacts struct {
	Cert *x509.Certificate
	Key *rsa.PrivateKey
	CertPEM []byte
	KeyPEM []byte
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
	caPem := secret.Data[caCertName]
	keyPem := secret.Data[caKeyName]
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
		Cert: caCert,
		CertPEM: caPem,
		KeyPEM: keyPem,
		Key: key,
	}, nil
}

// createCACert creates the self-signed CA cert and private key that will
// be used to sign the server certificate
func createCACert() (*KeyPairArtifacts, error) {
	now := time.Now()
	begin := now.Add(-1 * time.Hour)
	end := now.Add(10 * 365 * 24 * time.Hour)
	templ := &x509.Certificate{
		SerialNumber: big.NewInt(0),
		Subject: pkix.Name{
			CommonName: CAName,
			Organization: []string{"gatekeeper"},
		},
		NotBefore: begin,
		NotAfter: end,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA: true,
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
	begin := now.Add(-1* time.Hour)
	end := now.Add(10* 365 * 24 * time.Hour)
	templ := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: DNSName,
		},
		NotBefore: begin,
		NotAfter: end,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
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
	valid, err := validCert(cert, cert, key, CAName, lookaheadTime())
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
		return false, errors.New( "bad CA cert")
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
		DNSName: dnsName,
		Roots: pool,
		CurrentTime: at,
	})
	if err != nil {
		return false, errors.Wrap(err, "verifying cert")
	}
	return true, nil
}