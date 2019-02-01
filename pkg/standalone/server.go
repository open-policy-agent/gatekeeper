package standalone

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/open-policy-agent/kubernetes-policy-controller/pkg/opa"
	"github.com/open-policy-agent/kubernetes-policy-controller/pkg/webhook"
)

var (
	addrs             = flag.String("addr", ":7925", "set listening address of the server (e.g., [ip]:<port> for TCP)")
	tlsCertFile       = flag.String("tls-cert-file", "/certs/tls.crt", "set path of TLS certificate file. Only works in authorization mode.")
	tlsPrivateKeyFile = flag.String("tls-private-key-file", "/certs/tls.key", "set path of TLS private key file. Only works in authorization mode.")
)

// Serve launches a standalone server that does not depend on the Kubernetes API Server.
func Serve() error {
	srv := &Server{
		sMux: http.NewServeMux(),
	}
	opa := opa.NewFromFlags()
	webhook.AddGenericWebhooks(srv, opa)
	srv.addrs = strings.Split(*addrs, ",")

	cert, err := loadCertificate(*tlsCertFile, *tlsPrivateKeyFile)
	if err != nil {
		return err
	}
	srv.cert = cert

	loops, err := srv.Listeners()
	if err != nil {
		return err
	}

	errc := make(chan error)
	for _, loop := range loops {
		go func(serverLoop func() error) {
			errc <- serverLoop()
		}(loop)
	}
	for {
		select {
		case err := <-errc:
			return err
		}
	}
}

var _ webhook.GenericHandler = &Server{}

type Server struct {
	sMux  *http.ServeMux
	cert  *tls.Certificate
	addrs []string
}

// Handle registers an http handler with the server at the provided path.
func (s *Server) Handle(path string, h http.Handler) {
	s.sMux.Handle(path, h)
}

// Loop will contain all the calls from the server that we'll be listening on.
type Loop func() error

// Listeners returns functions that listen and serve connections.
func (s *Server) Listeners() ([]Loop, error) {
	loops := []Loop{}
	for _, addr := range s.addrs {
		parsedURL, err := parseURL(addr, s.cert != nil)
		if err != nil {
			return nil, err
		}
		var loop Loop
		switch parsedURL.Scheme {
		case "http":
			loop, err = s.getListenerForHTTPServer(parsedURL)
		case "https":
			loop, err = s.getListenerForHTTPSServer(parsedURL)
		default:
			err = fmt.Errorf("invalid url scheme %q", parsedURL.Scheme)
		}
		if err != nil {
			return nil, err
		}
		loops = append(loops, loop)
	}

	return loops, nil
}

func (s *Server) getListenerForHTTPServer(u *url.URL) (Loop, error) {
	httpServer := http.Server{
		Addr:    u.Host,
		Handler: s.sMux,
	}
	httpLoop := func() error { return httpServer.ListenAndServe() }

	return httpLoop, nil
}

func (s *Server) getListenerForHTTPSServer(u *url.URL) (Loop, error) {
	if s.cert == nil {
		return nil, fmt.Errorf("TLS certificate required but not supplied")
	}
	httpsServer := http.Server{
		Addr:    u.Host,
		Handler: s.sMux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{*s.cert},
		},
	}
	httpsLoop := func() error { return httpsServer.ListenAndServeTLS("", "") }

	return httpsLoop, nil
}

func parseURL(s string, useHTTPSByDefault bool) (*url.URL, error) {
	if !strings.Contains(s, "://") {
		scheme := "http://"
		if useHTTPSByDefault {
			scheme = "https://"
		}
		s = scheme + s
	}
	return url.Parse(s)
}

func loadCertificate(tlsCertFile, tlsPrivateKeyFile string) (*tls.Certificate, error) {

	if tlsCertFile != "" && tlsPrivateKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(tlsCertFile, tlsPrivateKeyFile)
		if err != nil {
			return nil, err
		}
		return &cert, nil
	} else if tlsCertFile != "" || tlsPrivateKeyFile != "" {
		return nil, fmt.Errorf("--tls-cert-file and --tls-private-key-file must be specified together")
	}

	return nil, nil
}
