module github.com/open-policy-agent/gatekeeper

go 1.16

require (
	contrib.go.opencensus.io/exporter/prometheus v0.3.0
	github.com/asaskevich/govalidator v0.0.0-20200907205600-7a23bdc65eef // indirect
	github.com/davecgh/go-spew v1.1.1
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.4.0
	github.com/go-logr/zapr v0.4.0
	github.com/go-openapi/spec v0.20.3 // indirect
	github.com/google/go-cmp v0.5.6
	github.com/google/uuid v1.1.2
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.16.0
	github.com/open-policy-agent/cert-controller v0.2.0
	github.com/open-policy-agent/frameworks/constraint v0.0.0-20210701194838-1dbe2618668d
	github.com/open-policy-agent/opa v0.32.1
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.11.0
	github.com/prometheus/common v0.30.0 // indirect
	github.com/prometheus/procfs v0.7.1 // indirect
	github.com/prometheus/statsd_exporter v0.21.0 // indirect
	github.com/spf13/cobra v1.2.1
	go.opencensus.io v0.23.0
	go.uber.org/zap v1.19.0
	golang.org/x/net v0.0.0-20210825183410-e898025ed96a
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/time v0.0.0-20210723032227-1f47c861a9ac
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b
	k8s.io/api v0.20.10
	k8s.io/apiextensions-apiserver v0.20.10
	k8s.io/apimachinery v0.20.10
	k8s.io/client-go v0.20.10
	k8s.io/klog/v2 v2.9.0
	k8s.io/utils v0.0.0-20210802155522-efc7438f0176
	sigs.k8s.io/controller-runtime v0.8.3
	sigs.k8s.io/yaml v1.2.0
)
