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

package readiness_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/open-policy-agent/gatekeeper/pkg/readiness"
	"github.com/pkg/errors"
)

const (
	certName       = "tls.crt"
	keyName        = "tls.key"
	caCertName     = "ca.crt"
	caName         = "ca"
	caOrganization = "org"
	certDir        = ""
	dnsName        = "service.namespace"
)

// Test_CertTracker periodically verifies the webhook tls cert is valid,
// the generated cert is valid before it's expired
// the readiness probe returns nil if valid, otherwise returns an error.
func Test_CertTracker(t *testing.T) {
	g := gomega.NewWithT(t)

	mgr, _ := setupManager(t)

	caArtifacts, err := createCACert()
	g.Expect(err).NotTo(gomega.HaveOccurred(), "creating ca cert")

	cert, key, err := createCertPEM(caArtifacts, 3*time.Second)
	g.Expect(err).NotTo(gomega.HaveOccurred(), "creating cert pem")

	caCrtFile := filepath.Join(certDir, caCertName)
	defer os.Remove(caCrtFile)
	if err != nil {
		t.Fatalf("expected error to be nil, got: %+v", err)
	}
	err = ioutil.WriteFile(caCrtFile, caArtifacts.CertPEM, 0644)
	if err != nil {
		t.Fatalf("expected error to be nil, got: %+v", err)
	}

	crtFile := filepath.Join(certDir, certName)
	defer os.Remove(crtFile)
	if err != nil {
		t.Fatalf("expected error to be nil, got: %+v", err)
	}
	err = ioutil.WriteFile(crtFile, cert, 0644)
	if err != nil {
		t.Fatalf("expected error to be nil, got: %+v", err)
	}

	keyFile := filepath.Join(certDir, keyName)
	defer os.Remove(keyFile)
	if err != nil {
		t.Fatalf("expected error to be nil, got: %+v", err)
	}
	err = ioutil.WriteFile(keyFile, key, 0644)
	if err != nil {
		t.Fatalf("expected error to be nil, got: %+v", err)
	}

	err = readiness.SetupCertTracker(mgr, certDir, dnsName)
	g.Expect(err).NotTo(gomega.HaveOccurred(), "setting up cert tracker")
	stopMgr, mgrStopped := StartTestManager(mgr, g)
	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	g.Eventually(func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return probeIsReady(ctx)
	}, 5*time.Second, 1*time.Second).Should(gomega.BeFalse())
}

// Test_CertTracker_NoFile verifies the webhook tls cert is valid,
// the readiness probe returns returns an error if there's no file.
func Test_CertTracker_NoFile(t *testing.T) {
	g := gomega.NewWithT(t)

	mgr, _ := setupManager(t)
	err := readiness.SetupCertTracker(mgr, "", "")
	g.Expect(err).NotTo(gomega.HaveOccurred(), "setting up cert tracker")

	stopMgr, mgrStopped := StartTestManager(mgr, g)
	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	g.Eventually(func() (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return probeIsReady(ctx)
	}, 3*time.Second, 1*time.Second).Should(gomega.BeFalse())
}

// KeyPairArtifacts stores cert artifacts.
type KeyPairArtifacts struct {
	Cert    *x509.Certificate
	Key     *rsa.PrivateKey
	CertPEM []byte
	KeyPEM  []byte
}

// createCACert creates the self-signed CA cert and private key that will
// be used to sign the server certificate
func createCACert() (*KeyPairArtifacts, error) {
	now := time.Now()
	begin := now.Add(-1 * time.Hour)
	end := now.Add(5 * time.Minute)
	templ := &x509.Certificate{
		SerialNumber: big.NewInt(0),
		Subject: pkix.Name{
			CommonName:   caName,
			Organization: []string{caOrganization},
		},
		DNSNames: []string{
			caName,
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
func createCertPEM(ca *KeyPairArtifacts, duration time.Duration) ([]byte, []byte, error) {
	now := time.Now()
	begin := now.Add(-1 * time.Hour)
	end := now.Add(duration)
	templ := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: dnsName,
		},
		DNSNames: []string{
			dnsName,
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
