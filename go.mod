module github.com/open-policy-agent/gatekeeper

go 1.15

require (
	contrib.go.opencensus.io/exporter/prometheus v0.1.0
	github.com/davecgh/go-spew v1.1.1
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.1.0
	github.com/go-logr/zapr v0.1.0
	github.com/google/go-cmp v0.5.0
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.1
	github.com/open-policy-agent/frameworks/constraint v0.0.0-20201020161305-2e11d4556af8
	github.com/open-policy-agent/opa v0.24.0
	github.com/open-policy-agent/cert-controller v0.0.0-20201021182510-6b649a1dbadc
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.1.0
	go.opencensus.io v0.22.2
	go.uber.org/zap v1.13.0
	golang.org/x/net v0.0.0-20201016165138-7b1cca2348c0
	golang.org/x/sync v0.0.0-20200625203802-6e8e738ad208
	gopkg.in/yaml.v2 v2.3.0
	k8s.io/api v0.18.6
	k8s.io/apiextensions-apiserver v0.18.6
	k8s.io/apimachinery v0.18.6
	k8s.io/client-go v0.18.6
	sigs.k8s.io/controller-runtime v0.6.3
	sigs.k8s.io/yaml v1.2.0
)
