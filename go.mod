module github.com/open-policy-agent/gatekeeper

go 1.16

require (
	contrib.go.opencensus.io/exporter/prometheus v0.1.0
	github.com/asaskevich/govalidator v0.0.0-20200907205600-7a23bdc65eef // indirect
	github.com/davecgh/go-spew v1.1.1
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.4.0
	github.com/go-logr/zapr v0.2.0
	github.com/go-openapi/spec v0.20.3 // indirect
	github.com/google/go-cmp v0.5.2
	github.com/google/uuid v1.1.2
	github.com/mitchellh/mapstructure v1.4.1 // indirect
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/open-policy-agent/cert-controller v0.2.0
	github.com/open-policy-agent/frameworks/constraint v0.0.0-20210522003146-5c034948ac29
	github.com/open-policy-agent/opa v0.24.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.7.1
	go.opencensus.io v0.22.3
	go.uber.org/zap v1.15.0
	golang.org/x/net v0.0.0-20210224082022-3d97a244fca7
	golang.org/x/sync v0.0.0-20201020160332-67f06af15bc9
	golang.org/x/time v0.0.0-20200630173020-3af7569d3a1e
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	k8s.io/api v0.20.2
	k8s.io/apiextensions-apiserver v0.20.2
	k8s.io/apimachinery v0.20.2
	k8s.io/client-go v0.20.2
	k8s.io/klog/v2 v2.8.0
	sigs.k8s.io/controller-runtime v0.8.2
	sigs.k8s.io/yaml v1.2.0
)
