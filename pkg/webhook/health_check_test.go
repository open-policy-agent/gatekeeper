package webhook

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTLSProbeHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
		want string
	}{
		{
			name: "defaults to ipv4 loopback",
			host: "",
			want: "127.0.0.1",
		},
		{
			name: "maps wildcard ipv4 to loopback",
			host: "0.0.0.0",
			want: "127.0.0.1",
		},
		{
			name: "maps wildcard ipv6 to loopback",
			host: "::",
			want: "::1",
		},
		{
			name: "unwraps bracketed ipv6",
			host: "[::1]",
			want: "::1",
		},
		{
			name: "preserves explicit host",
			host: "127.0.0.2",
			want: "127.0.0.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tlsProbeHost(tt.host))
		})
	}
}

func TestNewTLSChecker_UsesConfiguredHost(t *testing.T) {
	t.Parallel()

	certDir, port := startTestTLSServer(t, "127.0.0.2")
	checker := NewTLSChecker(certDir, "127.0.0.2", port)

	require.NoError(t, checker(nil))
}

func TestNewTLSChecker_UsesRequestContextCancellation(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	done := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-done
	}()
	defer close(done)

	host, portStr, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	checker := NewTLSChecker(t.TempDir(), host, port)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://probe.local", nil)
	require.NoError(t, err)

	start := time.Now()
	err = checker(req)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.ErrorContains(t, err, context.DeadlineExceeded.Error())
	require.Less(t, elapsed, time.Second)
}

func startTestTLSServer(t *testing.T, host string) (string, int) {
	t.Helper()

	certDir := t.TempDir()
	keyPair := writeTestServingCert(t, certDir, host)
	listener, err := tls.Listen("tcp", net.JoinHostPort(host, "0"), &tls.Config{
		Certificates: []tls.Certificate{keyPair},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Skipf("unable to bind test TLS listener on %s: %v", host, err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
	})

	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	return certDir, port
}

func writeTestServingCert(t *testing.T, certDir string, hosts ...string) tls.Certificate {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	certTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "gatekeeper-test-webhook",
		},
		DNSNames:              []string{},
		IPAddresses:           []net.IP{},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			certTemplate.IPAddresses = append(certTemplate.IPAddresses, ip)
			continue
		}
		certTemplate.DNSNames = append(certTemplate.DNSNames, host)
	}

	certDER, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	require.NoError(t, os.WriteFile(filepath.Join(certDir, "tls.crt"), certPEM, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(certDir, "tls.key"), keyPEM, 0o600))

	keyPair, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)

	return keyPair
}
