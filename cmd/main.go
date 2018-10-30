package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"

	"github.com/rite2nikhil/kubernetes-policy/pkg/opa"
	server "github.com/rite2nikhil/kubernetes-policy/pkg/server"
	"github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
)

var (
	defaultAddr = ":7925" // default listening address for server
)

// WhSvrParameters parameters
type parameters struct {
	// Addrs are the listening addresses that the OPA server will bind to.
	addrs *[]string
	// Certificate is the certificate to use in server-mode. If the certificate
	// is nil, the server will NOT use TLS.
	certificate *tls.Certificate
	opaaddress  string
}

var (
	opaaddress        = flag.String("opa-url", "http://localhost:8181/v1", "set URL of OPA API endpoint")
	addrs             = flag.StringSlice("addr", []string{defaultAddr}, "set listening address of the server (e.g., [ip]:<port> for TCP)")
	tlsCertFile       = flag.String("tls-cert-file", "/certs/tls.crt", "set path of TLS certificate file")
	tlsPrivateKeyFile = flag.String("tls-private-key-file", "/certs/tls.key", "set path of TLS private key file")
)

func main() {
	// get command line parameters
	flag.Parse()

	cert, err := loadCertificate(*tlsCertFile, *tlsPrivateKeyFile)
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	params := parameters{addrs: addrs, certificate: cert, opaaddress: *opaaddress}

	/*
		if err := server.InstallDefaultAdmissionPolicy("default-kubernetes-matches", types.KubernetesPolicy, opa.New(params.opaaddress)); err != nil {
			logrus.Fatalf("Failed to install default policy: %v", err)
		}
		if err := server.InstallDefaultAdmissionPolicy("default-policy-matches", types.PolicyMatchPolicy, opa.New(params.opaaddress)); err != nil {
			logrus.Fatalf("Failed to install default policy: %v", err)
		}
	*/

	ctx := context.Background()
	StartServer(ctx, params)
}

// StartServer starts the runtime in server mode. This function will block the calling goroutine.
func StartServer(ctx context.Context, params parameters) {

	logrus.WithFields(logrus.Fields{
		"addrs": params.addrs,
	}).Infof("First line of log stream.")

	s, err := server.New().
		WithAddresses(*params.addrs).
		WithCertificate(params.certificate).
		WithOPA(opa.New(params.opaaddress)).
		Init(ctx)

	if err != nil {
		logrus.WithField("err", err).Fatalf("Unable to initialize server.")
	}

	loops, err := s.Listeners()
	if err != nil {
		logrus.WithField("err", err).Fatalf("Unable to create listeners.")
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
			logrus.WithField("err", err).Fatal("Listener failed.")
		}
	}
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
