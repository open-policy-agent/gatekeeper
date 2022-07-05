module github.com/open-policy-agent/gatekeeper

go 1.16

replace golang.org/x/crypto => golang.org/x/crypto v0.0.0-20211202192323-5770296d904e // CVE-2021-43565

replace golang.org/x/text/language => golang.org/x/text/language v0.3.7 // CVE-2021-38561

require (
	contrib.go.opencensus.io/exporter/prometheus v0.1.0
	github.com/asaskevich/govalidator v0.0.0-20200907205600-7a23bdc65eef // indirect
	github.com/davecgh/go-spew v1.1.1
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.4.0
	github.com/go-logr/zapr v0.2.0
	github.com/go-openapi/spec v0.20.3 // indirect
	github.com/google/go-cmp v0.5.5
	github.com/google/uuid v1.1.2
	github.com/mitchellh/mapstructure v1.4.1 // indirect
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/open-policy-agent/cert-controller v0.2.0
	github.com/open-policy-agent/frameworks/constraint v0.0.0-20210803013759-9f2691290092
	github.com/open-policy-agent/opa v0.24.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.11.1
	go.opencensus.io v0.22.3
	go.uber.org/zap v1.15.0
	golang.org/x/net v0.0.0-20211112202133-69e39bad7dc2
	golang.org/x/sync v0.0.0-20201207232520-09787c993a3a
	golang.org/x/time v0.0.0-20200630173020-3af7569d3a1e
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	k8s.io/api v0.20.2
	k8s.io/apiextensions-apiserver v0.20.2
	k8s.io/apimachinery v0.20.2
	k8s.io/client-go v0.20.2
	k8s.io/klog/v2 v2.8.0
	sigs.k8s.io/controller-runtime v0.8.3
	sigs.k8s.io/yaml v1.2.0
)
