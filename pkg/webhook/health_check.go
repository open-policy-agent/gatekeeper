package webhook

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
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
	probeHosts := tlsProbeHosts(host)
	probeURLs := make([]string, 0, len(probeHosts))
	for _, probeHost := range probeHosts {
		probeURLs = append(probeURLs, fmt.Sprintf("https://%s", net.JoinHostPort(probeHost, strconv.Itoa(port))))
	}

	returnFunc := func(r *http.Request) error {
		ctx := context.Background()
		if r != nil {
			ctx = r.Context()
		}

		certPath := filepath.Join(certDir, "tls.crt")
		keyPath := filepath.Join(certDir, "tls.key")
		loadCert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			newErr := fmt.Errorf("unable to load certificate from certDir %s: %w", certDir, err)
			tlsCheckerLog.Error(newErr, "error in loading certificate")
			return newErr
		}

		var errs []error
		for _, probeURL := range probeURLs {
			if err := probeTLSURL(ctx, insecureClient, probeURL, loadCert.Certificate); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", probeURL, err))
				continue
			}

			return nil
		}

		newErr := fmt.Errorf("unable to verify webhook TLS endpoint: %w", errors.Join(errs...))
		tlsCheckerLog.Error(newErr, "error in checking webhook server with https")
		return newErr
	}
	return returnFunc
}

func probeTLSURL(ctx context.Context, insecureClient *http.Client, probeURL string, expectedCerts [][]byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return fmt.Errorf("unable to create probe request: %w", err)
	}

	resp, err := insecureClient.Do(req)
	if err != nil {
		return fmt.Errorf("unable to connect to server: %w", err)
	}
	defer resp.Body.Close()
	// explicitly discard the body to avoid any memory leak
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
		return fmt.Errorf("webhook does not serve TLS certificate")
	}

	serverCerts := resp.TLS.PeerCertificates
	// compare certificate in resp and the certificate in certDir
	if len(serverCerts) != len(expectedCerts) {
		return fmt.Errorf("server certificate chain length does not match certificate in certDir, %d vs %d", len(serverCerts), len(expectedCerts))
	}
	for i, serverCert := range serverCerts {
		if !bytes.Equal(serverCert.Raw, expectedCerts[i]) {
			return fmt.Errorf("server certificate %d does not match certificate %d in certDir", i, i)
		}
	}

	return nil
}

func tlsProbeHosts(host string) []string {
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	}

	if host == "" {
		return []string{"127.0.0.1", "::1"}
	}

	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		if ip.To4() != nil {
			return []string{"127.0.0.1"}
		}
		return []string{"::1"}
	}

	return []string{host}
}
