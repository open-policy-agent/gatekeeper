module github.com/open-policy-agent/gatekeeper

go 1.15

require (
	contrib.go.opencensus.io/exporter/prometheus v0.1.0
	github.com/davecgh/go-spew v1.1.1
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.1.0
	github.com/go-logr/zapr v0.1.0
	github.com/google/go-cmp v0.4.0
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.1
	github.com/open-policy-agent/cert-controller v0.0.0-20200921224206-24b87bbc4b6e
	github.com/open-policy-agent/frameworks/constraint v0.0.0-20200929072634-d96896eff389
	github.com/open-policy-agent/opa v0.21.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.1.0
	go.opencensus.io v0.22.2
	go.uber.org/zap v1.10.0
	golang.org/x/net v0.0.0-20200520004742-59133d7f0dd7
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e
	gopkg.in/yaml.v2 v2.3.0
	k8s.io/api v0.18.5
	k8s.io/apiextensions-apiserver v0.18.4
	k8s.io/apimachinery v0.18.5
	k8s.io/client-go v0.18.4
	sigs.k8s.io/controller-runtime v0.6.1
	sigs.k8s.io/yaml v1.2.0
)
