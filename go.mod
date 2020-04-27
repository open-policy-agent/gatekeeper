module github.com/open-policy-agent/gatekeeper

go 1.13

require (
	contrib.go.opencensus.io/exporter/prometheus v0.1.0
	github.com/davecgh/go-spew v1.1.1
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.1.0
	github.com/go-logr/zapr v0.1.0
	github.com/go-openapi/spec v0.19.4 // indirect
	github.com/google/go-cmp v0.3.1
	github.com/onsi/ginkgo v1.11.0
	github.com/onsi/gomega v1.8.1
	github.com/open-policy-agent/frameworks/constraint v0.0.0-20200420221412-5bae2037a343
	github.com/open-policy-agent/opa v0.19.1
	github.com/pkg/errors v0.8.1
	github.com/prometheus/client_golang v1.1.0
	go.opencensus.io v0.22.2
	go.uber.org/zap v1.10.0
	golang.org/x/net v0.0.0-20191004110552-13f9640d40b9
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e
	gopkg.in/yaml.v2 v2.2.8
	k8s.io/api v0.18.2
	k8s.io/apiextensions-apiserver v0.18.2
	k8s.io/apimachinery v0.18.2
	k8s.io/client-go v0.18.2
	sigs.k8s.io/controller-runtime v0.6.0
	sigs.k8s.io/yaml v1.2.0
)
