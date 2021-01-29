module github.com/open-policy-agent/gatekeeper

go 1.15

require (
	contrib.go.opencensus.io/exporter/prometheus v0.1.0
	github.com/davecgh/go-spew v1.1.1
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.3.0
	github.com/go-logr/zapr v0.2.0
	github.com/google/go-cmp v0.5.2
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/open-policy-agent/cert-controller v0.1.1-0.20210129015139-6ff9721a1c47
	github.com/open-policy-agent/frameworks/constraint v0.0.0-20210121003109-e55b2bb4cf1c
	github.com/open-policy-agent/opa v0.24.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.7.1
	go.opencensus.io v0.22.2
	go.uber.org/zap v1.15.0
	golang.org/x/net v0.0.0-20201016165138-7b1cca2348c0
	golang.org/x/sync v0.0.0-20200625203802-6e8e738ad208
	golang.org/x/time v0.0.0-20200630173020-3af7569d3a1e
	gopkg.in/yaml.v2 v2.3.0
	k8s.io/api v0.19.2
	k8s.io/apiextensions-apiserver v0.19.2
	k8s.io/apimachinery v0.19.2
	k8s.io/client-go v0.19.2
	sigs.k8s.io/controller-runtime v0.7.0
	sigs.k8s.io/yaml v1.2.0
)
