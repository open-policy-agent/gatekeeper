# Image URL to use all building/pushing image targets
REPOSITORY ?= openpolicyagent/gatekeeper
CRD_REPOSITORY ?= openpolicyagent/gatekeeper-crds
GATOR_REPOSITORY ?= openpolicyagent/gator
IMG := $(REPOSITORY):latest
CRD_IMG := $(CRD_REPOSITORY):latest
GATOR_IMG := $(GATOR_REPOSITORY):latest
# DEV_TAG will be replaced with short Git SHA on pre-release stage in CI
DEV_TAG ?= dev
USE_LOCAL_IMG ?= false
ENABLE_GENERATOR_EXPANSION ?= false
ENABLE_PUBSUB ?= false
AUDIT_CONNECTION ?= "audit"
AUDIT_CHANNEL ?= "audit"
LOG_LEVEL ?= "INFO"
GENERATE_VAP ?= false
GENERATE_VAPBINDING ?= false

VERSION := v3.18.0-beta.0

KIND_VERSION ?= 0.17.0
KIND_CLUSTER_FILE ?= test/bats/tests/kindcluster.yml
# note: k8s version pinned since KIND image availability lags k8s releases
KUBERNETES_VERSION ?= 1.30.0
KUSTOMIZE_VERSION ?= 3.8.9
BATS_VERSION ?= 1.8.2
ORAS_VERSION ?= 0.16.0
BATS_TESTS_FILE ?= test/bats/test.bats
HELM_VERSION ?= 3.7.2
NODE_VERSION ?= 16-bullseye-slim
YQ_VERSION ?= 4.30.6

HELM_ARGS ?=
GATEKEEPER_NAMESPACE ?= gatekeeper-system

# When updating this, make sure to update the corresponding action in
# workflow.yaml
GOLANGCI_LINT_VERSION := v1.57.1

# Detects the location of the user golangci-lint cache.
GOLANGCI_LINT_CACHE := $(shell pwd)/.tmp/golangci-lint

BENCHMARK_FILE_NAME ?= benchmarks.txt
FAKE_SUBSCRIBER_IMAGE ?= fake-subscriber:latest

ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
BIN_DIR := $(abspath $(ROOT_DIR)/bin)

LDFLAGS := "-X github.com/open-policy-agent/gatekeeper/v3/pkg/version.Version=$(VERSION)"

PLATFORM ?= linux/amd64
OUTPUT_TYPE ?= type=docker

MANAGER_IMAGE_PATCH := "apiVersion: apps/v1\
\nkind: Deployment\
\nmetadata:\
\n  name: controller-manager\
\n  namespace: system\
\nspec:\
\n  template:\
\n    spec:\
\n      containers:\
\n      - image: <your image file>\
\n        name: manager\
\n        args:\
\n        - --port=8443\
\n        - --logtostderr\
\n        - --emit-admission-events\
\n        - --admission-events-involved-namespace\
\n        - --exempt-namespace=${GATEKEEPER_NAMESPACE}\
\n        - --operation=webhook\
\n        - --operation=mutation-webhook\
\n        - --disable-opa-builtin=http.send\
\n        - --log-mutations\
\n        - --mutation-annotations\
\n        - --default-create-vap-for-templates=${GENERATE_VAP}\
\n        - --default-create-vap-binding-for-constraints=${GENERATE_VAPBINDING}\
\n        - --log-level=${LOG_LEVEL}\
\n---\
\napiVersion: apps/v1\
\nkind: Deployment\
\nmetadata:\
\n  name: audit\
\n  namespace: system\
\nspec:\
\n  template:\
\n    spec:\
\n      containers:\
\n      - image: <your image file>\
\n        name: manager\
\n        args:\
\n        - --emit-audit-events\
\n        - --audit-events-involved-namespace\
\n        - --operation=audit\
\n        - --operation=status\
\n        - --operation=mutation-status\
\n        - --audit-chunk-size=500\
\n        - --logtostderr\
\n        - --default-create-vap-for-templates=${GENERATE_VAP}\
\n        - --default-create-vap-binding-for-constraints=${GENERATE_VAPBINDING}\
\n        - --log-level=${LOG_LEVEL}\
\n"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

ifdef GENERATE_ATTESTATIONS
_ATTESTATIONS := --attest type=sbom --attest type=provenance,mode=max
endif

all: lint test manager

# Run tests
native-test: envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(KUBERNETES_VERSION) --bin-dir $(LOCALBIN) -p path)" \
	GO111MODULE=on \
	go test -mod vendor ./pkg/... ./apis/... ./cmd/gator/... -race -bench . -coverprofile cover.out

.PHONY: benchmark-test
benchmark-test:
	GOMAXPROCS=1 go test ./pkg/... -bench . -run="^#" -count 10 > ${BENCHMARK_FILE_NAME}

# Hook to run docker tests
.PHONY: test
test: __test-image
	docker run --rm -t -v $(shell pwd):/app \
		gatekeeper-test make native-test

.PHONY: test-e2e
test-e2e:
	bats -t ${BATS_TESTS_FILE}

.PHONY: test-gator
test-gator: gator test-gator-verify test-gator-test test-gator-expand

.PHONY: test-gator-containerized
test-gator-containerized: __test-image
	docker run --privileged -v $(shell pwd):/app -v /var/lib/docker \
	gatekeeper-test ./test/image/gator-test.sh

.PHONY: test-gator-verify
test-gator-verify: gator
	./bin/gator verify test/gator/verify/suite.yaml

.PHONY: test-gator-test
test-gator-test: gator
	bats test/gator/test

.PHONY: test-gator-expand
test-gator-expand: gator
	bats test/gator/expand

e2e-dependencies:
	# Download and install kind
	curl -L https://github.com/kubernetes-sigs/kind/releases/download/v${KIND_VERSION}/kind-linux-amd64 --output ${GITHUB_WORKSPACE}/bin/kind && chmod +x ${GITHUB_WORKSPACE}/bin/kind
	# Download and install kubectl
	curl -L https://dl.k8s.io/release/v${KUBERNETES_VERSION}/bin/linux/amd64/kubectl -o ${GITHUB_WORKSPACE}/bin/kubectl && chmod +x ${GITHUB_WORKSPACE}/bin/kubectl
	# Download and install kustomize
	curl -L https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv${KUSTOMIZE_VERSION}/kustomize_v${KUSTOMIZE_VERSION}_linux_amd64.tar.gz -o kustomize_v${KUSTOMIZE_VERSION}_linux_amd64.tar.gz && tar -zxvf kustomize_v${KUSTOMIZE_VERSION}_linux_amd64.tar.gz && chmod +x kustomize && mv kustomize ${GITHUB_WORKSPACE}/bin/kustomize
	# Download and install bats
	curl -sSLO https://github.com/bats-core/bats-core/archive/v${BATS_VERSION}.tar.gz && tar -zxvf v${BATS_VERSION}.tar.gz && bash bats-core-${BATS_VERSION}/install.sh ${GITHUB_WORKSPACE}
	# Install yq
	curl -L https://github.com/mikefarah/yq/releases/download/v$(YQ_VERSION)/yq_linux_amd64 -o ${GITHUB_WORKSPACE}/bin/yq && chmod +x ${GITHUB_WORKSPACE}/bin/yq

KIND_NODE_VERSION := kindest/node:v$(KUBERNETES_VERSION)
e2e-bootstrap: e2e-dependencies
	# Check for existing kind cluster
	if [ $$(${GITHUB_WORKSPACE}/bin/kind get clusters) ]; then ${GITHUB_WORKSPACE}/bin/kind delete cluster; fi

	# Create a new kind cluster
	# Only enabling VAP beta apis for 1.28, 1.29
	if [ $$(echo $(KUBERNETES_VERSION) | cut -d'.' -f2) -lt 28 ] || [ $$(echo $(KUBERNETES_VERSION) | cut -d'.' -f2) -gt 29 ]; then ${GITHUB_WORKSPACE}/bin/kind create cluster --image $(KIND_NODE_VERSION) --wait 5m; else ${GITHUB_WORKSPACE}/bin/kind create cluster --config $(KIND_CLUSTER_FILE) --image $(KIND_NODE_VERSION) --wait 5m; fi

e2e-build-load-image: docker-buildx e2e-build-load-externaldata-image
	kind load docker-image --name kind ${IMG} ${CRD_IMG}

e2e-build-load-externaldata-image: docker-buildx-builder
	./test/externaldata/dummy-provider/scripts/generate-tls-certificate.sh
	docker buildx build \
		--platform="$(PLATFORM)" \
		--output=$(OUTPUT_TYPE) \
		-t dummy-provider:test \
		-f test/externaldata/dummy-provider/Dockerfile test/externaldata/dummy-provider
	kind load docker-image --name kind dummy-provider:test

e2e-verify-release: e2e-build-load-externaldata-image patch-image deploy test-e2e
	echo -e '\n\n======= manager logs =======\n\n' && kubectl logs -n ${GATEKEEPER_NAMESPACE} -l control-plane=controller-manager

e2e-helm-install:
	rm -rf .staging/helm
	mkdir -p .staging/helm
	curl https://get.helm.sh/helm-v${HELM_VERSION}-linux-amd64.tar.gz > .staging/helm/helmbin.tar.gz
	cd .staging/helm && tar -xvf helmbin.tar.gz
	./.staging/helm/linux-amd64/helm version --client

e2e-helm-deploy: e2e-helm-install
ifeq ($(ENABLE_PUBSUB),true)
	./.staging/helm/linux-amd64/helm install manifest_staging/charts/gatekeeper --name-template=gatekeeper \
		--namespace ${GATEKEEPER_NAMESPACE} \
		--debug --wait \
		--set image.repository=${HELM_REPO} \
		--set image.crdRepository=${HELM_CRD_REPO} \
		--set image.release=${HELM_RELEASE} \
		--set postInstall.labelNamespace.image.repository=${HELM_CRD_REPO} \
		--set postInstall.labelNamespace.image.tag=${HELM_RELEASE} \
		--set postInstall.labelNamespace.enabled=true \
		--set postInstall.probeWebhook.enabled=true \
		--set emitAdmissionEvents=true \
		--set emitAuditEvents=true \
		--set admissionEventsInvolvedNamespace=true \
		--set auditEventsInvolvedNamespace=true \
		--set disabledBuiltins={http.send} \
		--set logMutations=true \
		--set audit.enablePubsub=${ENABLE_PUBSUB} \
		--set audit.connection=${AUDIT_CONNECTION} \
		--set audit.channel=${AUDIT_CHANNEL} \
		--set-string auditPodAnnotations.dapr\\.io/enabled=true \
		--set-string auditPodAnnotations.dapr\\.io/app-id=audit \
		--set-string auditPodAnnotations.dapr\\.io/metrics-port=9999 \
		--set logLevel=${LOG_LEVEL} \
		--set mutationAnnotations=true;
else
	./.staging/helm/linux-amd64/helm install manifest_staging/charts/gatekeeper --name-template=gatekeeper \
		--namespace ${GATEKEEPER_NAMESPACE} --create-namespace \
		--debug --wait \
		--set image.repository=${HELM_REPO} \
		--set image.crdRepository=${HELM_CRD_REPO} \
		--set image.release=${HELM_RELEASE} \
		--set postInstall.labelNamespace.image.repository=${HELM_CRD_REPO} \
		--set postInstall.labelNamespace.image.tag=${HELM_RELEASE} \
		--set postInstall.labelNamespace.enabled=true \
		--set postInstall.probeWebhook.enabled=true \
		--set emitAdmissionEvents=true \
		--set emitAuditEvents=true \
		--set admissionEventsInvolvedNamespace=true \
		--set auditEventsInvolvedNamespace=true \
		--set disabledBuiltins={http.send} \
		--set logMutations=true \
		--set logLevel=${LOG_LEVEL} \
		--set defaultCreateVAPForTemplates=${GENERATE_VAP} \
		--set defaultCreateVAPBindingForConstraints=${GENERATE_VAPBINDING} \
		--set mutationAnnotations=true;
endif

e2e-helm-upgrade-init: e2e-helm-install
	./.staging/helm/linux-amd64/helm repo add gatekeeper https://open-policy-agent.github.io/gatekeeper/charts;\
	./.staging/helm/linux-amd64/helm install gatekeeper gatekeeper/gatekeeper --version ${BASE_RELEASE} \
		--namespace ${GATEKEEPER_NAMESPACE} --create-namespace \
		--debug --wait \
		--set emitAdmissionEvents=true \
		--set emitAuditEvents=true \
		--set admissionEventsInvolvedNamespace=true \
		--set auditEventsInvolvedNamespace=true \
		--set postInstall.labelNamespace.enabled=true \
		--set postInstall.probeWebhook.enabled=true \
		--set disabledBuiltins={http.send} \
		--set enableExternalData=true \
		--set logMutations=true \
		--set logLevel=${LOG_LEVEL} \
		--set mutationAnnotations=true;\

e2e-helm-upgrade:
	./helm_migrate.sh
	./.staging/helm/linux-amd64/helm upgrade gatekeeper manifest_staging/charts/gatekeeper \
		--namespace ${GATEKEEPER_NAMESPACE} \
		--debug --wait \
		--set image.repository=${HELM_REPO} \
		--set image.crdRepository=${HELM_CRD_REPO} \
		--set image.release=${HELM_RELEASE} \
		--set postInstall.labelNamespace.image.repository=${HELM_CRD_REPO} \
		--set postInstall.labelNamespace.image.tag=${HELM_RELEASE} \
		--set postInstall.labelNamespace.enabled=true \
		--set postInstall.probeWebhook.enabled=true \
		--set emitAdmissionEvents=true \
		--set emitAuditEvents=true \
		--set admissionEventsInvolvedNamespace=true \
		--set auditEventsInvolvedNamespace=true \
		--set disabledBuiltins={http.send} \
		--set logMutations=true \
		--set logLevel=${LOG_LEVEL} \
		--set defaultCreateVAPForTemplates=${GENERATE_VAP} \
		--set defaultCreateVAPBindingForConstraints=${GENERATE_VAPBINDING} \
		--set mutationAnnotations=true;\

e2e-subscriber-build-load-image:
	docker buildx build --platform="linux/amd64" -t ${FAKE_SUBSCRIBER_IMAGE} --load -f test/pubsub/fake-subscriber/Dockerfile test/pubsub/fake-subscriber
	kind load docker-image --name kind ${FAKE_SUBSCRIBER_IMAGE}

e2e-subscriber-deploy:
	kubectl create ns fake-subscriber
	kubectl get secret redis --namespace=default -o yaml | sed 's/namespace: .*/namespace: fake-subscriber/' | kubectl apply -f -
	kubectl apply -f test/pubsub/fake-subscriber/manifest/subscriber.yaml

e2e-publisher-deploy:
	kubectl get secret redis --namespace=default -o yaml | sed 's/namespace: .*/namespace: gatekeeper-system/' | kubectl apply -f -
	kubectl apply -f test/pubsub/publish-components.yaml

# Build manager binary
manager: generate
	GO111MODULE=on go build -mod vendor -o bin/manager -ldflags $(LDFLAGS)

# Build manager binary
manager-osx: generate
	GO111MODULE=on go build -mod vendor -o bin/manager GOOS=darwin -ldflags $(LDFLAGS)

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate manifests
	GO111MODULE=on go run -mod vendor ./main.go

# Install CRDs into a cluster
install: manifests
	docker run -v $(shell pwd)/config:/config -v $(shell pwd)/vendor:/vendor \
		registry.k8s.io/kustomize/kustomize:v${KUSTOMIZE_VERSION} build \
		/config/crd | kubectl apply -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: patch-image manifests
ifeq ($(ENABLE_GENERATOR_EXPANSION),true)
	@grep -q -v 'enable-generator-resource-expansion' ./config/overlays/dev/manager_image_patch.yaml && sed -i '/- --operation=webhook/a \ \ \ \ \ \ \ \ - --enable-generator-resource-expansion=true' ./config/overlays/dev/manager_image_patch.yaml
	@grep -q -v 'enable-generator-resource-expansion' ./config/overlays/dev/manager_image_patch.yaml && sed -i '/- --operation=audit/a \ \ \ \ \ \ \ \ - --enable-generator-resource-expansion=true' ./config/overlays/dev/manager_image_patch.yaml
endif
	docker run \
		-v $(shell pwd)/config:/config \
		-v $(shell pwd)/vendor:/vendor \
		registry.k8s.io/kustomize/kustomize:v${KUSTOMIZE_VERSION} build \
		/config/overlays/dev | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: __controller-gen
	$(CONTROLLER_GEN) \
		crd \
		rbac:roleName=manager-role \
		webhook \
		paths="./apis/..." \
		paths="./pkg/..." \
		output:crd:artifacts:config=config/crd/bases
	./build/update-match-schema.sh
	rm -rf manifest_staging
	mkdir -p manifest_staging/deploy
	mkdir -p manifest_staging/charts/gatekeeper
	docker run --rm -v $(shell pwd):/gatekeeper \
		registry.k8s.io/kustomize/kustomize:v${KUSTOMIZE_VERSION} build \
		/gatekeeper/config/default -o /gatekeeper/manifest_staging/deploy/gatekeeper.yaml
	docker run --rm -v $(shell pwd):/gatekeeper \
		registry.k8s.io/kustomize/kustomize:v${KUSTOMIZE_VERSION} build \
		--load_restrictor LoadRestrictionsNone /gatekeeper/cmd/build/helmify | go run cmd/build/helmify/*.go

# lint runs a dockerized golangci-lint, and should give consistent results
# across systems.
# Source: https://golangci-lint.run/usage/install/#docker
lint:
	docker run -t --rm -v $(shell pwd):/app \
		-v ${GOLANGCI_LINT_CACHE}:/root/.cache/golangci-lint \
		-w /app golangci/golangci-lint:${GOLANGCI_LINT_VERSION} \
		golangci-lint run -v --fix

# Generate code
generate: __conversion-gen __controller-gen
	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths="./apis/..." paths="./pkg/..."
	$(CONVERSION_GEN) \
		--output-base=/gatekeeper \
		--input-dirs=./apis/mutations/v1,./apis/mutations/v1beta1,./apis/mutations/v1alpha1,./apis/expansion/v1alpha1,./apis/syncset/v1alpha1 \
		--go-header-file=./hack/boilerplate.go.txt \
		--output-file-base=zz_generated.conversion

# Prepare crds to be added to gatekeeper-crds image
clean-crds:
	rm -rf .staging/crds/*

build-crds: clean-crds
	mkdir -p .staging/crds
ifdef CI
	cp -R manifest_staging/charts/gatekeeper/crds/ .staging/crds/
else
	cp -R charts/gatekeeper/crds/ .staging/crds/
endif

# Docker Login
docker-login:
	@docker login -u $(DOCKER_USER) -p $(DOCKER_PASSWORD) $(REGISTRY)

docker-build: docker-buildx

docker-buildx-builder:
	if ! docker buildx ls | grep -q container-builder; then\
		docker buildx create --name container-builder --use --bootstrap;\
		docker buildx inspect;\
	fi

# Build image with buildx to build cross platform multi-architecture docker images
# https://docs.docker.com/buildx/working-with-buildx/
docker-buildx: docker-buildx-builder
	docker buildx build \
		$(_ATTESTATIONS) \
		--build-arg LDFLAGS=${LDFLAGS} \
		--platform="$(PLATFORM)" \
		--output=$(OUTPUT_TYPE) \
		-t $(IMG) .

docker-buildx-crds: build-crds docker-buildx-builder
	docker buildx build \
		$(_ATTESTATIONS) \
		--build-arg LDFLAGS=${LDFLAGS} \
		--platform="$(PLATFORM)" \
		--output=$(OUTPUT_TYPE) \
		-t $(CRD_IMG) \
		-f crd.Dockerfile .staging/crds/

docker-buildx-dev: docker-buildx-builder
	docker buildx build \
		$(_ATTESTATIONS) \
		--build-arg LDFLAGS=${LDFLAGS} \
		--platform="$(PLATFORM)" \
		--output=$(OUTPUT_TYPE) \
		-t $(REPOSITORY):$(DEV_TAG) \
		-t $(REPOSITORY):dev .

docker-buildx-crds-dev: build-crds docker-buildx-builder
	docker buildx build \
		$(_ATTESTATIONS) \
		--build-arg LDFLAGS=${LDFLAGS} \
		--platform="$(PLATFORM)" \
		--output=$(OUTPUT_TYPE) \
		-t $(CRD_REPOSITORY):$(DEV_TAG) \
		-t $(CRD_REPOSITORY):dev \
		-f crd.Dockerfile .staging/crds/

docker-buildx-release: docker-buildx-builder
	docker buildx build \
		$(_ATTESTATIONS) \
		--build-arg LDFLAGS=${LDFLAGS} \
		--platform="$(PLATFORM)" \
		--output=$(OUTPUT_TYPE) \
		-t $(REPOSITORY):$(VERSION) .

docker-buildx-crds-release: build-crds docker-buildx-builder
	docker buildx build \
		$(_ATTESTATIONS) \
		--build-arg LDFLAGS=${LDFLAGS}\
		--platform="$(PLATFORM)" \
		--output=$(OUTPUT_TYPE) \
		-t $(CRD_REPOSITORY):$(VERSION) \
		-f crd.Dockerfile .staging/crds/

# Build gator image
docker-buildx-gator-dev: docker-buildx-builder
	docker buildx build \
		$(_ATTESTATIONS) \
		--build-arg LDFLAGS=${LDFLAGS} \
		--platform="$(PLATFORM)" \
		--output=$(OUTPUT_TYPE) \
		-t ${GATOR_REPOSITORY}:${DEV_TAG} \
		-t ${GATOR_REPOSITORY}:dev \
		-f gator.Dockerfile .

docker-buildx-gator-release: docker-buildx-builder
	docker buildx build \
		$(_ATTESTATIONS) \
		--build-arg LDFLAGS=${LDFLAGS} \
		--platform="$(PLATFORM)" \
		--output=$(OUTPUT_TYPE) \
		-t ${GATOR_REPOSITORY}:${VERSION} \
		-f gator.Dockerfile .

# Update manager_image_patch.yaml with image tag
patch-image:
	@echo "updating kustomize image patch file for manager resource"
	@bash -c 'echo -e ${MANAGER_IMAGE_PATCH} > ./config/overlays/dev/manager_image_patch.yaml'
ifeq ($(USE_LOCAL_IMG),true)
	@sed -i '/^        name: manager/a \ \ \ \ \ \ \ \ imagePullPolicy: IfNotPresent' ./config/overlays/dev/manager_image_patch.yaml
endif
	@sed -i'' -e 's@image: .*@image: '"${IMG}"'@' ./config/overlays/dev/manager_image_patch.yaml

release-manifest:
	@sed -i'' -e 's@image: $(REPOSITORY):$(VERSION)@image: $(REPOSITORY):'"$(NEWVERSION)"'@' ./config/manager/manager.yaml
	@sed -i "s/appVersion: $(VERSION)/appVersion: ${NEWVERSION}/" ./cmd/build/helmify/static/Chart.yaml
	@sed -i "s/version: $$(echo ${VERSION} | cut -c2-)/version: $$(echo ${NEWVERSION} | cut -c2-)/" ./cmd/build/helmify/static/Chart.yaml
	@sed -i "s/release: $(VERSION)/release: ${NEWVERSION}/" ./cmd/build/helmify/static/values.yaml
	@sed -i "s/tag: $(VERSION)/tag: ${NEWVERSION}/" ./cmd/build/helmify/static/values.yaml
	@sed -i 's/Current release version: `$(VERSION)`/Current release version: `'"${NEWVERSION}"'`/' ./cmd/build/helmify/static/README.md
	@sed -i -e 's/^VERSION := $(VERSION)/VERSION := ${NEWVERSION}/' ./Makefile
	export
	$(MAKE) manifests

# Tags a new version for docs
.PHONY: version-docs
version-docs:
	docker run \
		-v $(shell pwd)/website:/website \
		-w /website \
		-u $(shell id -u):$(shell id -g) \
		node:${NODE_VERSION} \
		sh -c "yarn install --frozen-lockfile && yarn run docusaurus docs:version ${NEWVERSION}"
	@sed -i 's/https:\/\/raw\.githubusercontent\.com\/open-policy-agent\/gatekeeper\/master\/deploy\/gatekeeper\.yaml.*/https:\/\/raw\.githubusercontent\.com\/open-policy-agent\/gatekeeper\/${TAG}\/deploy\/gatekeeper\.yaml/' ./website/versioned_docs/version-${NEWVERSION}/install.md

.PHONY: patch-version-docs
patch-version-docs:
	@sed -i 's/https:\/\/raw\.githubusercontent\.com\/open-policy-agent\/gatekeeper\/${OLDVERSION}\/deploy\/gatekeeper\.yaml.*/https:\/\/raw\.githubusercontent\.com\/open-policy-agent\/gatekeeper\/${TAG}\/deploy\/gatekeeper\.yaml/' ./website/versioned_docs/version-${NEWVERSION}/install.md

promote-staging-manifest:
	@rm -rf deploy
	@cp -r manifest_staging/deploy .
	@rm -rf charts
	@cp -r manifest_staging/charts .

# Delete gatekeeper from a cluster. Note this is not a complete uninstall, just a dev convenience
uninstall:
	docker run -v $(shell pwd)/config:/config -v $(shell pwd)/vendor:/vendor \
		registry.k8s.io/kustomize/kustomize:v${KUSTOMIZE_VERSION} build \
		/config/overlays/dev | kubectl delete -f -

__controller-gen: __tooling-image
CONTROLLER_GEN=docker run --rm -v $(shell pwd):/gatekeeper gatekeeper-tooling controller-gen

__conversion-gen: __tooling-image
CONVERSION_GEN=docker run --rm -v $(shell pwd):/gatekeeper gatekeeper-tooling conversion-gen

__tooling-image:
	docker buildx build build/tooling \
		--platform="$(PLATFORM)" \
		--output=$(OUTPUT_TYPE) \
		-t gatekeeper-tooling

__test-image:
	docker buildx build test/image \
		--platform="$(PLATFORM)" \
		--output=$(OUTPUT_TYPE) \
		--build-arg YQ_VERSION=$(YQ_VERSION) \
		--build-arg BATS_VERSION=$(BATS_VERSION) \
		--build-arg ORAS_VERSION=$(ORAS_VERSION) \
		--build-arg KUSTOMIZE_VERSION=$(KUSTOMIZE_VERSION) \
		-t gatekeeper-test

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/.tmp/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

ENVTEST ?= $(LOCALBIN)/setup-envtest

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.0.0-20230118154835-9241bceb3098

.PHONY: vendor
vendor:
	go mod vendor
	go mod tidy

.PHONY: gator
gator: bin/gator-$(GOOS)-$(GOARCH)
	mv bin/gator-$(GOOS)-$(GOARCH) bin/gator

bin/gator-$(GOOS)-$(GOARCH):
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 GO111MODULE=on go build -o $(BIN_DIR)/gator-$(GOOS)-$(GOARCH) -ldflags $(LDFLAGS) ./cmd/gator

tilt-prepare:
	mkdir -p .tiltbuild/charts
	rm -rf .tiltbuild/charts/gatekeeper
	cp -R manifest_staging/charts/gatekeeper .tiltbuild/charts
	# disable some configs from the security context so we can perform live update
	sed -i -e "/readOnlyRootFilesystem: true/d" .tiltbuild/charts/gatekeeper/values.yaml
	sed -i -e "/run.*: .*/d" .tiltbuild/charts/gatekeeper/values.yaml

tilt: generate manifests tilt-prepare
	tilt up

tilt-clean:
	rm -rf .tiltbuild
