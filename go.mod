module github.com/open-policy-agent/gatekeeper

go 1.13

require (
	contrib.go.opencensus.io/exporter/prometheus v0.1.0
	github.com/davecgh/go-spew v1.1.1
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.1.0
	github.com/go-logr/zapr v0.1.0
	github.com/go-openapi/spec v0.19.4 // indirect
	github.com/go-openapi/strfmt v0.19.3 // indirect
	github.com/go-openapi/validate v0.19.4 // indirect
	github.com/google/go-cmp v0.3.1
	github.com/onsi/ginkgo v1.10.1 // indirect
	github.com/onsi/gomega v1.7.0
	github.com/open-policy-agent/frameworks/constraint v0.0.0-20200127222620-69dff9b895a2
	github.com/open-policy-agent/opa v0.16.2
	github.com/pkg/errors v0.8.1
	go.opencensus.io v0.22.2
	go.uber.org/zap v1.10.0
	golang.org/x/net v0.0.0-20190827160401-ba9fcec4b297
	k8s.io/api v0.16.4
	k8s.io/apiextensions-apiserver v0.16.4
	k8s.io/apimachinery v0.16.4
	k8s.io/client-go v0.16.4
	sigs.k8s.io/controller-runtime v0.4.0
	sigs.k8s.io/controller-tools v0.2.2 // indirect
)
