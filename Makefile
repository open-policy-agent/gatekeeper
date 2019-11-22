
# Image URL to use all building/pushing image targets
REGISTRY ?= quay.io
REPOSITORY ?= $(REGISTRY)/open-policy-agent/gatekeeper

IMG := $(REPOSITORY):latest

VERSION := v3.0.4-beta.2

USE_LOCAL_IMG ?= false
KIND_VERSION=0.6.0
KUSTOMIZE_VERSION=3.0.2

BUILD_COMMIT := $(shell ./build/get-build-commit.sh)
BUILD_TIMESTAMP := $(shell ./build/get-build-timestamp.sh)
BUILD_HOSTNAME := $(shell ./build/get-build-hostname.sh)

LDFLAGS := "-X github.com/open-policy-agent/gatekeeper/version.Version=$(VERSION) \
	-X github.com/open-policy-agent/gatekeeper/version.Vcs=$(BUILD_COMMIT) \
	-X github.com/open-policy-agent/gatekeeper/version.Timestamp=$(BUILD_TIMESTAMP) \
	-X github.com/open-policy-agent/gatekeeper/version.Hostname=$(BUILD_HOSTNAME)"

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
\n        name: manager"

FRAMEWORK_PACKAGE := github.com/open-policy-agent/frameworks/constraint

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= crd:trivialVersions=true

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: test manager

# Run tests
native-test: generate fmt vet manifests
	GO111MODULE=on go test -mod vendor ./pkg/... -coverprofile cover.out

# Hook to run docker tests
.PHONY: test
test:
	rm -rf .staging/test
	mkdir -p .staging/test
	cp -r * .staging/test
	-rm .staging/test/Dockerfile
	cp test/Dockerfile .staging/test/Dockerfile
	docker build --pull .staging/test -t gatekeeper-test && docker run -t gatekeeper-test

test-e2e:
	bats -t test/bats/test.bats

e2e-bootstrap:
	# Download and install kind
	curl -L https://github.com/kubernetes-sigs/kind/releases/download/v${KIND_VERSION}/kind-linux-amd64 --output kind && chmod +x kind && sudo mv kind /usr/local/bin/
	# Download and install kubectl
	curl -LO https://storage.googleapis.com/kubernetes-release/release/$$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl && chmod +x ./kubectl && sudo mv kubectl /usr/local/bin/
	# Download and install kustomize
	curl -L https://github.com/kubernetes-sigs/kustomize/releases/download/v${KUSTOMIZE_VERSION}/kustomize_${KUSTOMIZE_VERSION}_linux_amd64 --output kustomize && chmod +x kustomize && sudo mv kustomize /usr/local/bin/
	# Check for existing kind cluster
	if [ $$(kind get clusters) ]; then kind delete cluster; fi
	# Create a new kind cluster
	TERM=dumb kind create cluster

e2e-build-load-image: docker-build
	kind load docker-image --name kind ${IMG}

e2e-verify-release: patch-image deploy test-e2e
	echo -e '\n\n======= manager logs =======\n\n' && kubectl logs -n gatekeeper-system -l control-plane=controller-manager

# Build manager binary
manager: generate fmt vet
	GO111MODULE=on go build -mod vendor -o bin/manager -ldflags $(LDFLAGS) main.go

# Build manager binary
manager-osx: generate fmt vet
	GO111MODULE=on go build -mod vendor -o bin/manager GOOS=darwin  -ldflags $(LDFLAGS) main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet manifests
	GO111MODULE=on go run -mod vendor ./main.go

# Install CRDs into a cluster
install: manifests
	kustomize build config/crd | kubectl apply -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests
	touch -a ./config/overlays/dev/manager_image_patch.yaml
# TODO use kustomize for CRDs
	kubectl apply -f config/crd/bases
	kubectl apply -f vendor/${FRAMEWORK_PACKAGE}/deploy
	kustomize build config/overlays/dev | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./api/..." paths="./pkg/..." output:crd:artifacts:config=config/crd/bases
	kustomize build config/default  -o deploy/gatekeeper_kubebuilder_v2.yaml
	bash -c 'for x in vendor/${FRAMEWORK_PACKAGE}/deploy/*.yaml ; do echo --- >> deploy/gatekeeper_kubebuilder_v2.yaml ; cat $${x} >> deploy/gatekeeper_kubebuilder_v2.yaml ; done'

# Run go fmt against code
fmt:
	GO111MODULE=on go fmt ./api/... ./pkg/...
	GO111MODULE=on go fmt main.go

# Run go vet against code
vet:
	GO111MODULE=on go vet -mod vendor ./api/... ./pkg/...
	GO111MODULE=on go vet -mod vendor main.go

# Generate code
generate: controller-gen target-template-source
	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths="./api/..." paths="./pkg/..."

# Docker Login
docker-login:
	@docker login -u $(DOCKER_USER) -p $(DOCKER_PASSWORD) $(REGISTRY)

# Tag for Dev
docker-tag-dev:
	@docker tag $(IMG) $(REPOSITORY):dev

# Tag for Dev
docker-tag-release:
	@docker tag $(IMG) $(REPOSITORY):$(VERSION)
	@docker tag $(IMG) $(REPOSITORY):latest

# Push for Dev
docker-push-dev:  docker-tag-dev
	@docker push $(REPOSITORY):dev

# Push for Release
docker-push-release:  docker-tag-release
	@docker push $(REPOSITORY):$(VERSION)
	@docker push $(REPOSITORY):latest

# Build the docker image
docker-build: test
	docker build --pull . -t ${IMG}

# Update manager_image_patch.yaml with image tag
patch-image:
	@echo "updating kustomize image patch file for manager resource"
	@test -s ./config/overlays/dev/manager_image_patch.yaml || bash -c 'echo -e ${MANAGER_IMAGE_PATCH} > ./config/overlays/dev/manager_image_patch.yaml'
ifeq ($(USE_LOCAL_IMG),true)
	@sed -i '/^        name: manager/a \ \ \ \ \ \ \ \ imagePullPolicy: IfNotPresent' ./config/overlays/dev/manager_image_patch.yaml
endif
	@sed -i'' -e 's@image: .*@image: '"${IMG}"'@' ./config/overlays/dev/manager_image_patch.yaml

# Rebuild pkg/target/target_template_source.go to pull in pkg/target/regolib/src.rego
target-template-source:
	@printf "package target\n\n// This file is generated from pkg/target/regolib/src.rego via \"make target-template-source\"\n// Do not modify this file directly!\n\nconst templSrc = \`" > pkg/target/target_template_source.go
	@sed -e "s/data\[\"{{.DataRoot}}\"\]/{{.DataRoot}}/; s/data\[\"{{.ConstraintsRoot}}\"\]/{{.ConstraintsRoot}}/" pkg/target/regolib/src.rego >> pkg/target/target_template_source.go
	@printf "\`\n" >> pkg/target/target_template_source.go

docker-build-ci:
	docker build --pull . -t $(IMG) -f Dockerfile_ci

# Push the docker image
docker-push:
	docker push ${IMG}

release:
	@sed -i -e 's/^VERSION := .*/VERSION := ${NEWVERSION}/' ./Makefile

release-manifest:
		@sed -i'' -e 's@image: $(REPOSITORY):.*@image: $(REPOSITORY):'"$(NEWVERSION)"'@' ./config/manager/manager.yaml ./deploy/gatekeeper_kubebuilder_v2.yaml


# Travis Dev Deployment
travis-dev-deploy: docker-login docker-build-ci docker-push-dev

# Travis Release
travis-release-deploy: docker-login docker-build-ci docker-push-release

# Delete gatekeeper from a cluster. Note this is not a complete uninstall, just a dev convenience
uninstall:
	-kubectl delete -n gatekeeper-system Config config
	sleep 5
	kubectl delete ns gatekeeper-system

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	GO111MODULE=on go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.2.2
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

.PHONY: vendor
vendor:
	$(eval $@_TMP := $(shell mktemp -d))
	$(eval $@_CACHE := ${$@_TMP}/pkg/mod/cache/download)
	GO111MODULE=on go mod download
	GO111MODULE=on GOPROXY=file://${GOPATH}/pkg/mod/cache/download GOPATH=${$@_TMP} go mod download
	GO111MODULE=on GOPROXY=file://${$@_CACHE} go mod vendor
	$(eval $@_PACKAGE := $(shell GO111MODULE=on go mod graph | awk '{print $$2}' | grep '^${FRAMEWORK_PACKAGE}@'))
	mkdir -p vendor/${FRAMEWORK_PACKAGE}/deploy
	cp -r ${$@_TMP}/pkg/mod/${$@_PACKAGE}/deploy/* vendor/${FRAMEWORK_PACKAGE}/deploy/.
