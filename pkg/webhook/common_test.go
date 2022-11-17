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
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func TestCongifureWebhookServer(t *testing.T) {
	expectedServer := &webhook.Server{
		TLSMinVersion: "1.3",
	}

	if *clientCAName != "" {
		expectedServer.ClientCAName = *clientCAName
	}

	tc := []struct {
		Name           string
		Server         *webhook.Server
		ExpectedServer *webhook.Server
	}{
		{
			Name:           "Wbhook server config",
			Server:         &webhook.Server{},
			ExpectedServer: expectedServer,
		},
	}

	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			server := congifureWebhookServer(tt.Server)
			expectedServer.TLSOpts = server.TLSOpts

			if !reflect.DeepEqual(tt.ExpectedServer, server) {
				t.Errorf(fmt.Sprintf("got %#v, want %#v", server, tt.ExpectedServer))
			}
		})
	}
}

func TestTLSConfig(t *testing.T) {
	ca, caPEM, caPrivKey, err := getCA(*certCNName)
	if err != nil {
		t.Fatal(err)
	}

	certpool := x509.NewCertPool()
	certpool.AppendCertsFromPEM(caPEM.Bytes())

	serverTLSConf, err := serverCertSetup(*certCNName, ca, caPrivKey, certpool)
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "success!")
	}))
	ts.TLS = serverTLSConf
	ts.StartTLS()
	defer ts.Close()

	goodHTTPClient, err := getClient(*certCNName, ca, caPrivKey, certpool)
	if err != nil {
		t.Fatal(err)
	}

	badHTTPClient, err := getClient("test", ca, caPrivKey, certpool)
	if err != nil {
		t.Fatal(err)
	}

	tc := []struct {
		Name      string
		WantError bool
		Msg       string
		Client    *http.Client
	}{
		{
			Name:      "Connecting to server with valid certificate that has expected CN name",
			WantError: false,
			Msg:       "success!",
			Client:    goodHTTPClient,
		},
		{
			Name:      "Connecting to server with unvalid certificate that has unexpected CN name",
			WantError: true,
			Msg:       "",
			Client:    badHTTPClient,
		},
	}

	for _, tt := range tc {
		t.Run(tt.Name, func(t *testing.T) {
			resp, err := tt.Client.Get(ts.URL)
			assert.Equal(t, err != nil, tt.WantError)

			if !tt.WantError {
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
}

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

	// create our private and public key
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

func serverCertSetup(s string, ca *x509.Certificate, caPrivKey *rsa.PrivateKey, certPool *x509.CertPool) (serverTLSConf *tls.Config, err error) {
	// set up our server certificate
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
		VerifyConnection: getCertNameVerifier(),
		ClientCAs:        certPool,
		ClientAuth:       tls.RequireAndVerifyClientCert,
		MinVersion:       tls.VersionTLS13,
	}
	return
}

func getClient(s string, ca *x509.Certificate, caPrivKey *rsa.PrivateKey, certPool *x509.CertPool) (httpClient *http.Client, err error) {
	// set up our client certificate
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
