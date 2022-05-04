# Image URL to use all building/pushing image targets
REPOSITORY ?= openpolicyagent/gatekeeper
CRD_REPOSITORY ?= openpolicyagent/gatekeeper-crds
IMG := $(REPOSITORY):latest
CRD_IMG := $(CRD_REPOSITORY):latest
# DEV_TAG will be replaced with short Git SHA on pre-release stage in CI
DEV_TAG ?= dev
USE_LOCAL_IMG ?= false
ENABLE_EXTERNAL_DATA ?= false

VERSION := v3.8.1

KIND_VERSION ?= 0.11.0
# note: k8s version pinned since KIND image availability lags k8s releases
KUBERNETES_VERSION ?= 1.23.0
KUSTOMIZE_VERSION ?= 3.8.9
BATS_VERSION ?= 1.2.1
BATS_TESTS_FILE ?= test/bats/test.bats
HELM_VERSION ?= 3.7.2
NODE_VERSION ?= 16-bullseye-slim
YQ_VERSION ?= 4.2.0

HELM_ARGS ?=
GATEKEEPER_NAMESPACE ?= gatekeeper-system

# When updating this, make sure to update the corresponding action in
# workflow.yaml
GOLANGCI_LINT_VERSION := v1.45.2

# Detects the location of the user golangci-lint cache.
GOLANGCI_LINT_CACHE := $(shell pwd)/.tmp/golangci-lint

ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
BIN_DIR := $(abspath $(ROOT_DIR)/bin)

BUILD_COMMIT := $(shell ./build/get-build-commit.sh)
BUILD_TIMESTAMP := $(shell ./build/get-build-timestamp.sh)
BUILD_HOSTNAME := $(shell ./build/get-build-hostname.sh)

LDFLAGS := "-X github.com/open-policy-agent/gatekeeper/pkg/version.Version=$(VERSION) \
	-X github.com/open-policy-agent/gatekeeper/pkg/version.Vcs=$(BUILD_COMMIT) \
	-X github.com/open-policy-agent/gatekeeper/pkg/version.Timestamp=$(BUILD_TIMESTAMP) \
	-X github.com/open-policy-agent/gatekeeper/pkg/version.Hostname=$(BUILD_HOSTNAME)"

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
\n        - --exempt-namespace=${GATEKEEPER_NAMESPACE}\
\n        - --operation=webhook\
\n        - --operation=mutation-webhook\
\n        - --disable-opa-builtin=http.send\
\n        - --log-mutations\
\n        - --mutation-annotations\
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
\n        - --operation=audit\
\n        - --operation=status\
\n        - --operation=mutation-status\
\n        - --audit-chunk-size=500\
\n        - --logtostderr"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: lint test manager

# Run tests
native-test:
	GO111MODULE=on go test -mod vendor ./pkg/... ./apis/... -bench . -coverprofile cover.out

# Hook to run docker tests
.PHONY: test
test:
	rm -rf .staging/test
	mkdir -p .staging/test
	cp -r * .staging/test
	-rm .staging/test/Dockerfile
	cp test/Dockerfile .staging/test/Dockerfile
	docker build --pull .staging/test -t gatekeeper-test && docker run -t gatekeeper-test

.PHONY: test-e2e
test-e2e:
	bats -t ${BATS_TESTS_FILE}

.PHONY: test-gator
test-gator: gator test-gator-verify test-gator-test

.PHONY: test-gator-verify
test-gator-verify: gator
	./bin/gator verify test/gator/verify/suite.yaml

.PHONY: test-gator-test
test-gator-test: gator
	bats test/gator/test

e2e-dependencies:
	# Download and install kind
	curl -L https://github.com/kubernetes-sigs/kind/releases/download/v${KIND_VERSION}/kind-linux-amd64 --output ${GITHUB_WORKSPACE}/bin/kind && chmod +x ${GITHUB_WORKSPACE}/bin/kind
	# Download and install kubectl
	curl -L https://storage.googleapis.com/kubernetes-release/release/v${KUBERNETES_VERSION}/bin/linux/amd64/kubectl -o ${GITHUB_WORKSPACE}/bin/kubectl && chmod +x ${GITHUB_WORKSPACE}/bin/kubectl
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
	TERM=dumb ${GITHUB_WORKSPACE}/bin/kind create cluster --image $(KIND_NODE_VERSION) --wait 5m

e2e-build-load-image: docker-buildx
	kind load docker-image --name kind ${IMG} ${CRD_IMG}

e2e-build-load-externaldata-image: docker-buildx-builder
	docker buildx build --platform="linux/amd64" -t dummy-provider:test --load -f test/externaldata/dummy-provider/Dockerfile test/externaldata/dummy-provider
	kind load docker-image --name kind dummy-provider:test

e2e-verify-release: patch-image deploy test-e2e
	echo -e '\n\n======= manager logs =======\n\n' && kubectl logs -n ${GATEKEEPER_NAMESPACE} -l control-plane=controller-manager

e2e-helm-install:
	rm -rf .staging/helm
	mkdir -p .staging/helm
	curl https://get.helm.sh/helm-v${HELM_VERSION}-linux-amd64.tar.gz > .staging/helm/helmbin.tar.gz
	cd .staging/helm && tar -xvf helmbin.tar.gz
	./.staging/helm/linux-amd64/helm version --client

e2e-helm-deploy: e2e-helm-install
	./.staging/helm/linux-amd64/helm install manifest_staging/charts/gatekeeper --name-template=gatekeeper \
		--namespace ${GATEKEEPER_NAMESPACE} --create-namespace \
		--debug --wait \
		--set image.repository=${HELM_REPO} \
		--set image.crdRepository=${HELM_CRD_REPO} \
		--set image.release=${HELM_RELEASE} \
		--set postInstall.labelNamespace.image.repository=${HELM_CRD_REPO} \
		--set postInstall.labelNamespace.image.tag=${HELM_RELEASE} \
		--set postInstall.labelNamespace.enabled=true \
		--set emitAdmissionEvents=true \
		--set emitAuditEvents=true \
		--set disabledBuiltins={http.send} \
		--set logMutations=true \
		--set mutationAnnotations=true;\

e2e-helm-upgrade-init: e2e-helm-install
	./.staging/helm/linux-amd64/helm repo add gatekeeper https://open-policy-agent.github.io/gatekeeper/charts;\
	./.staging/helm/linux-amd64/helm install gatekeeper gatekeeper/gatekeeper --version ${BASE_RELEASE} \
		--namespace ${GATEKEEPER_NAMESPACE} --create-namespace \
		--debug --wait \
		--set emitAdmissionEvents=true \
		--set emitAuditEvents=true \
		--set postInstall.labelNamespace.enabled=true \
		--set disabledBuiltins={http.send};\

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
		--set emitAdmissionEvents=true \
		--set emitAuditEvents=true \
		--set disabledBuiltins={http.send} \
		--set logMutations=true \
		--set mutationAnnotations=true;\

# Build manager binary
manager: generate
	GO111MODULE=on go build -mod vendor -o bin/manager -ldflags $(LDFLAGS) main.go

# Build manager binary
manager-osx: generate
	GO111MODULE=on go build -mod vendor -o bin/manager GOOS=darwin -ldflags $(LDFLAGS) main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate manifests
	GO111MODULE=on go run -mod vendor ./main.go

# Install CRDs into a cluster
install: manifests
	docker run -v $(shell pwd)/config:/config -v $(shell pwd)/vendor:/vendor \
		k8s.gcr.io/kustomize/kustomize:v${KUSTOMIZE_VERSION} build \
		/config/crd | kubectl apply -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: patch-image manifests
ifeq ($(ENABLE_EXTERNAL_DATA),true)
	@grep -q -v 'enable-external-data' ./config/overlays/dev/manager_image_patch.yaml && sed -i '/- --operation=webhook/a \ \ \ \ \ \ \ \ - --enable-external-data=true' ./config/overlays/dev/manager_image_patch.yaml
	@grep -q -v 'enable-external-data' ./config/overlays/dev/manager_image_patch.yaml && sed -i '/- --operation=audit/a \ \ \ \ \ \ \ \ - --enable-external-data=true' ./config/overlays/dev/manager_image_patch.yaml
endif
	docker run -v $(shell pwd)/config:/config -v $(shell pwd)/vendor:/vendor \
		k8s.gcr.io/kustomize/kustomize:v${KUSTOMIZE_VERSION} build \
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
	rm -rf manifest_staging
	mkdir -p manifest_staging/deploy/experimental
	mkdir -p manifest_staging/charts/gatekeeper
	docker run --rm -v $(shell pwd):/gatekeeper \
		k8s.gcr.io/kustomize/kustomize:v${KUSTOMIZE_VERSION} build \
		/gatekeeper/config/default -o /gatekeeper/manifest_staging/deploy/gatekeeper.yaml
	docker run --rm -v $(shell pwd):/gatekeeper \
		k8s.gcr.io/kustomize/kustomize:v${KUSTOMIZE_VERSION} build \
		--load_restrictor LoadRestrictionsNone /gatekeeper/cmd/build/helmify | go run cmd/build/helmify/*.go

# lint runs a dockerized golangci-lint, and should give consistent results
# across systems.
# Source: https://golangci-lint.run/usage/install/#docker
lint:
	docker run --rm -v $(shell pwd):/app \
		-v ${GOLANGCI_LINT_CACHE}:/root/.cache/golangci-lint \
		-w /app golangci/golangci-lint:${GOLANGCI_LINT_VERSION}-alpine \
		golangci-lint run -v

# Generate code
generate: __conversion-gen __controller-gen
	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths="./apis/..." paths="./pkg/..."
	$(CONVERSION_GEN) \
		--output-base=/gatekeeper \
		--input-dirs=./apis/mutations/v1beta1,./apis/mutations/v1alpha1 \
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

# Tag for Dev
docker-tag-dev:
	@docker tag $(IMG) $(REPOSITORY):$(DEV_TAG)
	@docker tag $(IMG) $(REPOSITORY):dev
	@docker tag $(CRD_IMG) $(CRD_REPOSITORY):$(DEV_TAG)
	@docker tag $(CRD_IMG) $(CRD_REPOSITORY):dev

# Tag for Dev
docker-tag-release:
	@docker tag $(IMG) $(REPOSITORY):$(VERSION)
	@docker tag $(CRD_IMG) $(CRD_REPOSITORY):$(VERSION)

# Push for Dev
docker-push-dev: docker-tag-dev
	@docker push $(REPOSITORY):$(DEV_TAG)
	@docker push $(REPOSITORY):dev
	@docker push $(CRD_REPOSITORY):$(DEV_TAG)
	@docker push $(CRD_REPOSITORY):dev

# Push for Release
docker-push-release: docker-tag-release
	@docker push $(REPOSITORY):$(VERSION)
	@docker push $(CRD_REPOSITORY):$(VERSION)

# Add crds to gatekeeper-crds image
# Build gatekeeper image
docker-build: build-crds
	docker build --pull -f crd.Dockerfile .staging/crds/ --build-arg LDFLAGS=${LDFLAGS} --build-arg KUBE_VERSION=${KUBERNETES_VERSION} --build-arg TARGETOS="linux" --build-arg TARGETARCH="amd64" -t ${CRD_IMG}
	docker build --pull . --build-arg LDFLAGS=${LDFLAGS} -t ${IMG}

docker-buildx-builder:
	if ! docker buildx ls | grep -q container-builder; then\
		docker buildx create --name container-builder --use;\
	fi

# Build image with buildx to build cross platform multi-architecture docker images
# https://docs.docker.com/buildx/working-with-buildx/
docker-buildx: build-crds docker-buildx-builder
	docker buildx build --build-arg LDFLAGS=${LDFLAGS} --platform "linux/amd64" \
		-t $(IMG) \
		. --load
	docker buildx build --build-arg LDFLAGS=${LDFLAGS} --build-arg KUBE_VERSION=${KUBERNETES_VERSION} --platform "linux/amd64" \
		-t $(CRD_IMG) \
		-f crd.Dockerfile .staging/crds/ --load

docker-buildx-dev: docker-buildx-builder
	docker buildx build --build-arg LDFLAGS=${LDFLAGS} --platform "linux/amd64,linux/arm64,linux/arm/v7" \
		-t $(REPOSITORY):$(DEV_TAG) \
		-t $(REPOSITORY):dev \
		. --push

docker-buildx-crds-dev: build-crds docker-buildx-builder
	docker buildx build --build-arg LDFLAGS=${LDFLAGS} --build-arg KUBE_VERSION=${KUBERNETES_VERSION} --platform "linux/amd64,linux/arm64,linux/arm/v7" \
		-t $(CRD_REPOSITORY):$(DEV_TAG) \
		-t $(CRD_REPOSITORY):dev \
		-f crd.Dockerfile .staging/crds/ --push

docker-buildx-release: docker-buildx-builder
	docker buildx build --build-arg LDFLAGS=${LDFLAGS} --platform "linux/amd64,linux/arm64,linux/arm/v7" \
		-t $(REPOSITORY):$(VERSION) \
		. --push

docker-buildx-crds-release: build-crds docker-buildx-builder
	docker buildx build --build-arg LDFLAGS=${LDFLAGS} --build-arg KUBE_VERSION=${KUBERNETES_VERSION} --platform "linux/amd64,linux/arm64,linux/arm/v7" \
		-t $(CRD_REPOSITORY):$(VERSION) \
		-f crd.Dockerfile .staging/crds/ --push

# Update manager_image_patch.yaml with image tag
patch-image:
	@echo "updating kustomize image patch file for manager resource"
	@bash -c 'echo -e ${MANAGER_IMAGE_PATCH} > ./config/overlays/dev/manager_image_patch.yaml'
ifeq ($(USE_LOCAL_IMG),true)
	@sed -i '/^        name: manager/a \ \ \ \ \ \ \ \ imagePullPolicy: IfNotPresent' ./config/overlays/dev/manager_image_patch.yaml
endif
	@sed -i'' -e 's@image: .*@image: '"${IMG}"'@' ./config/overlays/dev/manager_image_patch.yaml

# Push the docker image
docker-push:
	docker push ${IMG}
	docker push ${CRD_IMG}

release-manifest:
	@sed -i -e 's/^VERSION := .*/VERSION := ${NEWVERSION}/' ./Makefile
	@sed -i'' -e 's@image: $(REPOSITORY):.*@image: $(REPOSITORY):'"$(NEWVERSION)"'@' ./config/manager/manager.yaml
	@sed -i "s/appVersion: .*/appVersion: ${NEWVERSION}/" ./cmd/build/helmify/static/Chart.yaml
	@sed -i "s/version: .*/version: $$(echo ${NEWVERSION} | cut -c2-)/" ./cmd/build/helmify/static/Chart.yaml
	@sed -i "s/release: .*/release: ${NEWVERSION}/" ./cmd/build/helmify/static/values.yaml
	@sed -i "s/tag: .*/tag: ${NEWVERSION}/" ./cmd/build/helmify/static/values.yaml
	@sed -i 's/Current release version: `.*`/Current release version: `'"${NEWVERSION}"'`/' ./cmd/build/helmify/static/README.md
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

promote-staging-manifest:
	@rm -rf deploy
	@cp -r manifest_staging/deploy .
	@rm -rf charts
	@cp -r manifest_staging/charts .

# Delete gatekeeper from a cluster. Note this is not a complete uninstall, just a dev convenience
uninstall:
	docker run -v $(shell pwd)/config:/config -v $(shell pwd)/vendor:/vendor \
		k8s.gcr.io/kustomize/kustomize:v${KUSTOMIZE_VERSION} build \
		/config/overlays/dev | kubectl delete -f -

__controller-gen: __tooling-image
CONTROLLER_GEN=docker run -v $(shell pwd):/gatekeeper gatekeeper-tooling controller-gen

__conversion-gen: __tooling-image
CONVERSION_GEN=docker run -v $(shell pwd):/gatekeeper gatekeeper-tooling conversion-gen

__tooling-image:
	docker build . \
		-t gatekeeper-tooling \
		-f build/tooling/Dockerfile

.PHONY: vendor
vendor:
	go mod vendor
	go mod tidy

.PHONY: gator
gator: bin/gator-$(GOOS)-$(GOARCH)
	mv bin/gator-$(GOOS)-$(GOARCH) bin/gator

bin/gator-$(GOOS)-$(GOARCH):
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(BIN_DIR)/gator-$(GOOS)-$(GOARCH) -ldflags $(LDFLAGS) ./cmd/gator

tilt-prepare:
	mkdir -p .tiltbuild/charts
	rm -rf .tiltbuild/charts/gatekeeper
	cp -R manifest_staging/charts/gatekeeper .tiltbuild/charts
	# disable some configs from the security context so we can perform live update
	sed -i -e "/readOnlyRootFilesystem: true/d" .tiltbuild/charts/gatekeeper/templates/*.yaml
	sed -i -e "/run.*: .*/d" .tiltbuild/charts/gatekeeper/templates/*.yaml

tilt: generate manifests tilt-prepare
	tilt up

tilt-clean:
	rm -rf .tiltbuild
