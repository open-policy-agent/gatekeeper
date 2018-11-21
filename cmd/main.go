package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"

	"github.com/Azure/kubernetes-policy-controller/pkg/opa"
	"github.com/Azure/kubernetes-policy-controller/pkg/server"
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
	certificate  *tls.Certificate

	logLevel     string

	// CAs to use for the communication with OPA, per default the system certs are used
	// if the flag opa-ca-file is set the CA certs from this file are also used
	opaCAs       *x509.CertPool
	// Addresse of OPA
	opaAddress   string
	// auth token which is used via Bearer scheme to communicate with OPA
	opaAuthToken string
}

var (
	addrs             = flag.StringSlice("addr", []string{defaultAddr}, "set listening address of the server (e.g., [ip]:<port> for TCP)")
	tlsCertFile       = flag.String("tls-cert-file", "/certs/tls.crt", "set path of TLS certificate file")
	tlsPrivateKeyFile = flag.String("tls-private-key-file", "/certs/tls.key", "set path of TLS private key file")
	opaAddress        = flag.String("opa-url", "http://localhost:8181/v1", "set URL of OPA API endpoint")
	opaCAFile         = flag.String("opa-ca-file", "", "set path of the ca crts which are used in addition to the system certs to verify the opa server cert")
	opaAuthTokenFile  = flag.String("opa-auth-token-file", "", "set path of auth token file where the bearer token for opa is stored in format 'token = \"<auth token>\"'")
	logLevel          = flag.String("log-level", "info", "log level, which can be: panic, fatal, error, warn, info or debug")
)

func main() {
	// get command line parameters
	flag.Parse()

	cert, err := loadCertificate(*tlsCertFile, *tlsPrivateKeyFile)
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}

	opaAuthToken, err := loadOpaAuthToken(*opaAuthTokenFile)
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}

	opaCAs, err := loadCACertificates(*opaCAFile)
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}

	params := parameters{
		addrs:        addrs,
		certificate:  cert,
		opaAddress:   *opaAddress,
		opaCAs:       opaCAs,
		opaAuthToken: opaAuthToken,
		logLevel:     *logLevel,
	}

	/*
		if err := server.InstallDefaultAdmissionPolicy("default-kubernetes-matches", types.KubernetesPolicy, opa.New(params.opaAddress)); err != nil {
			logrus.Fatalf("Failed to install default policy: %v", err)
		}
		if err := server.InstallDefaultAdmissionPolicy("default-policy-matches", types.PolicyMatchPolicy, opa.New(params.opaAddress)); err != nil {
			logrus.Fatalf("Failed to install default policy: %v", err)
		}
	*/

	ctx := context.Background()
	StartServer(ctx, params)
}

// StartServer starts the runtime in server mode. This function will block the calling goroutine.
func StartServer(ctx context.Context, params parameters) {

	loglevel, err := logrus.ParseLevel(params.logLevel)
	if err != nil {
		logrus.WithField("err", err).Fatalf("Unable to parse log level.")
	}
	logrus.SetLevel(loglevel)

	logrus.WithFields(logrus.Fields{
		"addrs": params.addrs,
	}).Infof("First line of log stream.")

	s, err := server.New().
		WithAddresses(*params.addrs).
		WithCertificate(params.certificate).
		WithOPA(opa.New(params.opaAddress, params.opaCAs, params.opaAuthToken)).
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

func loadCACertificates(tlsCertFile string) (*x509.CertPool, error) {
	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	if tlsCertFile != "" {
		certs, err := ioutil.ReadFile(tlsCertFile)
		if err != nil {
			return nil, fmt.Errorf("could not load ca certificate: %v", err)
		}

		// Append our cert to the system pool
		if ok := rootCAs.AppendCertsFromPEM(certs); !ok {
			logrus.Info("No certs appended, using system certs only")
		}
	}

	return rootCAs, nil
}

var opaAuthTokenRegex = regexp.MustCompile("token.*=.*\"(.*)\"")

func loadOpaAuthToken(opaAuthTokenFile string) (string, error) {
	if opaAuthTokenFile == "" {
		return "", nil
	}

	bytes, err := ioutil.ReadFile(opaAuthTokenFile)
	if err != nil {
		return "", fmt.Errorf("error reading opaAuthTokenFile: %v", err)
	}

	match := opaAuthTokenRegex.FindStringSubmatch(string(bytes))
	if len(match) != 2 {
		return "", fmt.Errorf("error matching token in opaAuthTokenFile, matched: %v", match)
	}

	return match[1], nil
}
