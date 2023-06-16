package webhook

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	lg "log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type chanWriter chan string

func (w chanWriter) Write(p []byte) (n int, err error) {
	w <- string(p)
	return len(p), nil
}

func TestTLSConfig(t *testing.T) {
	ca, caPEM, caPrivKey, err := getCA(*CertCNName)
	if err != nil {
		t.Fatal(err)
	}

	certpool := x509.NewCertPool()
	certpool.AppendCertsFromPEM(caPEM.Bytes())

	serverTLSConf, err := serverCertSetup(*CertCNName, ca, caPrivKey, certpool)
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "success!")
	}))
	ts.TLS = serverTLSConf
	errc := make(chanWriter, 10)
	ts.Config.ErrorLog = lg.New(errc, "", 0)
	ts.StartTLS()
	defer ts.Close()

	goodHTTPClient, err := getClient(*CertCNName, ca, caPrivKey, certpool)
	if err != nil {
		t.Fatal(err)
	}

	badHTTPClient, err := getClient("test", ca, caPrivKey, certpool)
	if err != nil {
		t.Fatal(err)
	}

	diffCa, diffCaPEM, diffCaPrivKey, err := getCA(*CertCNName)
	if err != nil {
		t.Fatal(err)
	}

	diffCertpool := x509.NewCertPool()
	diffCertpool.AppendCertsFromPEM(diffCaPEM.Bytes())

	diffHTTPClient, err := getClient(*CertCNName, diffCa, diffCaPrivKey, diffCertpool)
	if err != nil {
		t.Fatal(err)
	}

	tc := []struct {
		Name      string
		WantError bool
		Msg       string
		Client    *http.Client
		Error     error
	}{
		{
			Name:      "Connecting to server with valid certificate that has expected CN name",
			WantError: false,
			Msg:       "success!",
			Client:    goodHTTPClient,
			Error:     nil,
		},
		{
			Name:      "Connecting to server with valid certificate but with wrong CN name",
			WantError: true,
			Msg:       "",
			Client:    badHTTPClient,
			Error:     fmt.Errorf("Get \"%s\": remote error: tls: bad certificate", ts.URL),
		},
		{
			Name:      "Connecting to server with invalid certificate that has expected CN name",
			WantError: true,
			Msg:       "",
			Client:    diffHTTPClient,
			Error:     fmt.Errorf("x509: certificate signed by unknown authority"),
		},
	}

	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			resp, err := tt.Client.Get(ts.URL)
			assert.Equal(t, err != nil, tt.WantError)
			if tt.WantError {
				assert.ErrorContains(t, err, tt.Error.Error())
			} else {
				// verify the response
				respBodyBytes, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				body := strings.TrimSpace(string(respBodyBytes))
				assert.Equal(t, body, tt.Msg)
			}
		})
	}

	select {
	case v := <-errc:
		if !strings.Contains(v, fmt.Sprintf("x509: subject with cn=test do not identify as %s", *CertCNName)) {
			t.Errorf("expected an error log message containing '%s'; got %q", fmt.Sprintf("x509: subject with cn=test do not identify as %s", *CertCNName), v)
		}
	case <-time.After(5 * time.Second):
		t.Errorf("timeout waiting for logged error")
	}
}

// returns a CA, CA PEM and private key.
func getCA(s string) (ca *x509.Certificate, caPEM *bytes.Buffer, caPrivKey *rsa.PrivateKey, err error) {
	// set up our CA certificate
	ca = &x509.Certificate{
		SerialNumber: big.NewInt(2019),
		Subject: pkix.Name{
			CommonName: s,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	// create private and public key
	caPrivKey, err = rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, nil, err
	}

	// create the CA
	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return nil, nil, nil, err
	}

	// pem encode
	caPEM = new(bytes.Buffer)
	err = pem.Encode(caPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	caPrivKeyPEM := new(bytes.Buffer)
	err = pem.Encode(caPrivKeyPEM, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(caPrivKey),
	})
	if err != nil {
		return nil, nil, nil, err
	}

	return
}

// return server TLS configurations.
func serverCertSetup(s string, ca *x509.Certificate, caPrivKey *rsa.PrivateKey, certPool *x509.CertPool) (serverTLSConf *tls.Config, err error) {
	// set up server certificate
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(2019),
		Subject: pkix.Name{
			CommonName: s,
		},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		SubjectKeyId: []byte{1, 2, 3, 4, 6},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	certPrivKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, err
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, cert, ca, &certPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return nil, err
	}

	certPEM := new(bytes.Buffer)
	err = pem.Encode(certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})
	if err != nil {
		return nil, err
	}

	certPrivKeyPEM := new(bytes.Buffer)
	err = pem.Encode(certPrivKeyPEM, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(certPrivKey),
	})
	if err != nil {
		return nil, err
	}

	serverCert, err := tls.X509KeyPair(certPEM.Bytes(), certPrivKeyPEM.Bytes())
	if err != nil {
		return nil, err
	}

	serverTLSConf = &tls.Config{
		Certificates:     []tls.Certificate{serverCert},
		VerifyConnection: GetCertNameVerifier(),
		ClientCAs:        certPool,
		ClientAuth:       tls.RequireAndVerifyClientCert,
		MinVersion:       tls.VersionTLS13,
	}
	return
}

// returns http client that can connect to TLS server.
func getClient(s string, ca *x509.Certificate, caPrivKey *rsa.PrivateKey, certPool *x509.CertPool) (httpClient *http.Client, err error) {
	// set up client certificate
	client := &x509.Certificate{
		SerialNumber: big.NewInt(2019),
		Subject: pkix.Name{
			CommonName: s,
		},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		SubjectKeyId: []byte{1, 2, 3, 4, 6},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	clientCertPrivKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, err
	}

	clientCertBytes, err := x509.CreateCertificate(rand.Reader, client, ca, &clientCertPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return nil, err
	}

	clientCertPEM := new(bytes.Buffer)
	err = pem.Encode(clientCertPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: clientCertBytes,
	})
	if err != nil {
		return nil, err
	}

	clientCertPrivKeyPEM := new(bytes.Buffer)
	err = pem.Encode(clientCertPrivKeyPEM, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(clientCertPrivKey),
	})
	if err != nil {
		return nil, err
	}

	clientCert, err := tls.X509KeyPair(clientCertPEM.Bytes(), clientCertPrivKeyPEM.Bytes())
	if err != nil {
		return nil, err
	}

	clientTLSConf := &tls.Config{
		RootCAs:      certPool,
		Certificates: []tls.Certificate{clientCert},
		MinVersion:   tls.VersionTLS13,
	}

	transport := &http.Transport{
		TLSClientConfig: clientTLSConf,
	}

	httpClient = &http.Client{
		Transport: transport,
	}

	return
}
