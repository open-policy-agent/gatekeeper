package webhook

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

var (
	cr = &CertRotator{
		CAName:         "ca",
		CAOrganization: "org",
		DNSName:        "service.namespace",
	}
)

func TestCertSigning(t *testing.T) {
	caArtifacts, err := cr.createCACert()
	if err != nil {
		t.Fatal(err)
	}

	cert, key, err := cr.createCertPEM(caArtifacts)
	if err != nil {
		t.Fatal(err)
	}

	if !cr.validServerCert(caArtifacts.CertPEM, cert, key) {
		t.Error("Generated cert is not valid")
	}
}

func TestCertExpiry(t *testing.T) {
	caArtifacts, err := cr.createCACert()
	if err != nil {
		t.Fatal(err)
	}

	cert, key, err := cr.createCertPEM(caArtifacts)
	if err != nil {
		t.Fatal(err)
	}

	if !cr.validServerCert(caArtifacts.CertPEM, cert, key) {
		t.Error("Generated cert is not valid")
	}

	valid, err := validCert(caArtifacts.CertPEM, cert, key, cr.DNSName, time.Now().Add(11*365*24*time.Hour))
	if err == nil {
		t.Error("Generated cert has not expired when it should have")
	}
	if valid {
		t.Error("Expired cert is still valid")
	}
}

func TestBadCA(t *testing.T) {
	caArtifacts, err := cr.createCACert()
	if err != nil {
		t.Fatal(err)
	}

	cert, key, err := cr.createCertPEM(caArtifacts)
	if err != nil {
		t.Fatal(err)
	}

	badCAArtifacts, err := cr.createCACert()
	if err != nil {
		t.Fatal(err)
	}

	if cr.validServerCert(badCAArtifacts.CertPEM, cert, key) {
		t.Error("Generated cert is valid when it should not be")
	}
}

func TestSelfSignedCA(t *testing.T) {
	caArtifacts, err := cr.createCACert()
	if err != nil {
		t.Fatal(err)
	}

	if !cr.validCACert(caArtifacts.CertPEM, caArtifacts.KeyPEM) {
		t.Error("Generated cert is not valid")
	}
}

func TestCAExpiry(t *testing.T) {
	caArtifacts, err := cr.createCACert()
	if err != nil {
		t.Fatal(err)
	}

	if !cr.validCACert(caArtifacts.CertPEM, caArtifacts.KeyPEM) {
		t.Error("Generated cert is not valid")
	}

	valid, err := validCert(caArtifacts.CertPEM, caArtifacts.CertPEM, caArtifacts.KeyPEM, cr.CAName, time.Now().Add(11*365*24*time.Hour))
	if err == nil {
		t.Error("Generated cert has not expired when it should have")
	}
	if valid {
		t.Error("Expired cert is still valid")
	}
}

func TestSecretRoundTrip(t *testing.T) {
	caArtifacts, err := cr.createCACert()
	if err != nil {
		t.Fatal(err)
	}

	cert, key, err := cr.createCertPEM(caArtifacts)
	if err != nil {
		t.Fatal(err)
	}

	if !cr.validServerCert(caArtifacts.CertPEM, cert, key) {
		t.Fatal("Generated cert is not valid")
	}

	secret := &corev1.Secret{}
	populateSecret(cert, key, caArtifacts, secret)
	art2, err := buildArtifactsFromSecret(secret)
	if err != nil {
		t.Fatal(err)
	}

	if !cr.validServerCert(art2.CertPEM, cert, key) {
		t.Fatal("Recovered cert is not valid")
	}

	cert2, key2, err := cr.createCertPEM(art2)
	if err != nil {
		t.Fatal(err)
	}

	if !cr.validServerCert(caArtifacts.CertPEM, cert2, key2) {
		t.Fatal("Second generated cert is not valid")
	}
}

func TestEmptyIsInvalid(t *testing.T) {
	if cr.validServerCert([]byte{}, []byte{}, []byte{}) {
		t.Fatal("empty cert is valid")
	}
	if cr.validCACert([]byte{}, []byte{}) {
		t.Fatal("empty CA cert is valid")
	}
}
