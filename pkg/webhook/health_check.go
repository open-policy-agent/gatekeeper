package webhook

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var tlsCheckerLog = logf.Log.WithName("webhook-tls-checker")

func NewTLSChecker(certDir string, port int) func(*http.Request) error {
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

	returnFunc := func(_ *http.Request) error {
		resp, err := insecureClient.Get(fmt.Sprintf("https://127.0.0.1:%d", port))
		if err != nil {
			newErr := fmt.Errorf("unable to connect to server: %w", err)
			tlsCheckerLog.Error(newErr, "error in connecting to webhook server with https")
			return newErr
		}
		defer resp.Body.Close()
		// explicitly discard the body to avoid any memory leak
		_, _ = io.Copy(io.Discard, resp.Body)

		if len(resp.TLS.PeerCertificates) == 0 {
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
