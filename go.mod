module github.com/open-policy-agent/gatekeeper

go 1.16

require (
	contrib.go.opencensus.io/exporter/prometheus v0.4.0
	github.com/davecgh/go-spew v1.1.1
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.4.0
	github.com/go-logr/zapr v0.4.0
	github.com/google/go-cmp v0.5.7
	github.com/google/uuid v1.3.0
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.17.0
	github.com/open-policy-agent/cert-controller v0.2.0
	github.com/open-policy-agent/frameworks/constraint v0.0.0-20220121182312-5d06dedcafb4
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.11.0
	github.com/spf13/cobra v1.2.1
	go.opencensus.io v0.23.0
	go.uber.org/zap v1.19.1
	golang.org/x/net v0.0.0-20211203184738-4852103109b8
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/time v0.0.0-20211116232009-f0f3c7e86c11
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b
	k8s.io/api v0.21.4
	k8s.io/apiextensions-apiserver v0.21.4
	k8s.io/apimachinery v0.21.4
	k8s.io/client-go v0.21.4
	k8s.io/klog/v2 v2.10.0
	k8s.io/utils v0.0.0-20211203121628-587287796c64
	sigs.k8s.io/controller-runtime v0.9.7
	sigs.k8s.io/yaml v1.3.0
)
