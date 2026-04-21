package webhook

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var tlsCheckerLog = logf.Log.WithName("webhook-tls-checker")

func NewTLSChecker(certDir, host string, port int) func(*http.Request) error {
	//nolint:forcetypeassert
	tr := http.DefaultTransport.(*http.Transport).Clone()
	// disabling gosec linting here as the http client used in this checking is intended to skip CA verification
	//
	//nolint:gosec
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	// disable keep alives to ensure that http connection aren't reused, otherwise the check may
	// fail if the cert was rotated in between
	tr.DisableKeepAlives = true
	insecureClient := &http.Client{Transport: tr}
	probeURL := fmt.Sprintf("https://%s", net.JoinHostPort(tlsProbeHost(host), strconv.Itoa(port)))

	returnFunc := func(r *http.Request) error {
		ctx := context.Background()
		if r != nil {
			ctx = r.Context()
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
		if err != nil {
			newErr := fmt.Errorf("unable to create probe request: %w", err)
			tlsCheckerLog.Error(newErr, "error creating https request for webhook server")
			return newErr
		}

		resp, err := insecureClient.Do(req)
		if err != nil {
			newErr := fmt.Errorf("unable to connect to server: %w", err)
			tlsCheckerLog.Error(newErr, "error in connecting to webhook server with https")
			return newErr
		}
		defer resp.Body.Close()
		// explicitly discard the body to avoid any memory leak
		_, _ = io.Copy(io.Discard, resp.Body)

		if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
			newErr := fmt.Errorf("webhook does not serve TLS certificate")
			tlsCheckerLog.Error(newErr, "error in connecting to webhook server with https")
			return newErr
		}
		serverCerts := resp.TLS.PeerCertificates
		certPath := filepath.Join(certDir, "tls.crt")
		keyPath := filepath.Join(certDir, "tls.key")
		loadCert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			newErr := fmt.Errorf("unable to load certificate from certDir %s: %w", certDir, err)
			tlsCheckerLog.Error(newErr, "error in loading certificate")
			return newErr
		}
		// compare certificate in resp and the certificate in certDir
		if len(serverCerts) != len(loadCert.Certificate) {
			newErr := fmt.Errorf("server certificate chain length does not match certificate in certDir, %d vs %d", len(serverCerts), len(loadCert.Certificate))
			tlsCheckerLog.Error(newErr, "certificate chain mismatch")
			return newErr
		}
		for i, serverCert := range serverCerts {
			if !bytes.Equal(serverCert.Raw, loadCert.Certificate[i]) {
				newErr := fmt.Errorf("server certificate %d does not match certificate %d in certDir", i, i)
				tlsCheckerLog.Error(newErr, "certificate chain mismatch")
				return newErr
			}
		}

		return nil
	}
	return returnFunc
}

func tlsProbeHost(host string) string {
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	}

	if host == "" {
		return "127.0.0.1"
	}

	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		if ip.To4() != nil {
			return "127.0.0.1"
		}
		return "::1"
	}

	return host
}
