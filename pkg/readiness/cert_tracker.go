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

package readiness

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"net/http"
	"path/filepath"

	"github.com/pkg/errors"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var ctLog = logf.Log.WithName("cert-tracker")

// CertTracker tracks readiness for certs of the webhook.
type CertTracker struct {
	certDir string
	dnsName string
}

// NewCertTracker creates a new CertTracker
func NewCertTracker(certDir string, dnsName string) *CertTracker {
	return &CertTracker{
		certDir: certDir,
		dnsName: dnsName,
	}
}

// CheckCert implements healthz.Checker to report readiness based on cert validity
// the readiness probe returns nil if valid, otherwise returns an error.
func (c *CertTracker) CheckCert(req *http.Request) error {
	ctLog.V(1).Info("readiness checker CheckCert started")

	// Load files
	caCrt, err := ioutil.ReadFile(filepath.Join(c.certDir, "ca.crt"))
	if err != nil {
		return errors.Wrap(err, "Unable to open CA cert")
	}

	tlsCrt, err := ioutil.ReadFile(filepath.Join(c.certDir, "tls.crt"))
	if err != nil {
		return errors.Wrap(err, "Unable to open tls crt")
	}
	tlsKey, err := ioutil.ReadFile(filepath.Join(c.certDir, "tls.key"))
	if err != nil {
		return errors.Wrap(err, "Unable to open tls key")
	}
	err = ValidCert(caCrt, tlsCrt, tlsKey, c.dnsName)
	if err != nil {
		return errors.Wrap(err, "readiness checker CheckCert certs not valid")
	}
	ctLog.V(1).Info("readiness checker CheckCert completed")
	return nil
}

// ValidCert checks validity of cert
func ValidCert(caCert, cert, key []byte, dnsName string) error {
	if len(caCert) == 0 || len(cert) == 0 || len(key) == 0 {
		return errors.New("empty cert")
	}

	pool := x509.NewCertPool()
	caDer, _ := pem.Decode(caCert)
	if caDer == nil {
		return errors.New("bad CA cert")
	}
	cac, err := x509.ParseCertificate(caDer.Bytes)
	if err != nil {
		return errors.Wrap(err, "parsing CA cert")
	}
	pool.AddCert(cac)

	_, err = tls.X509KeyPair(cert, key)
	if err != nil {
		return errors.Wrap(err, "building key pair")
	}

	b, _ := pem.Decode(cert)
	if b == nil {
		return errors.New("bad private key")
	}

	crt, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		return errors.Wrap(err, "parsing cert")
	}
	_, err = crt.Verify(x509.VerifyOptions{
		DNSName: dnsName,
		Roots:   pool,
	})
	if err != nil {
		return errors.Wrap(err, "verifying cert")
	}
	return nil
}
