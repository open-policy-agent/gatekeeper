# Image URL to use all building/pushing image targets
REPOSITORY ?= openpolicyagent/gatekeeper

IMG := $(REPOSITORY):latest
# DEV_TAG will be replaced with short Git SHA on pre-release stage in CI
DEV_TAG ?= dev

VERSION := v3.1.0-beta.10

USE_LOCAL_IMG ?= false
KIND_VERSION=0.7.0
KUSTOMIZE_VERSION=3.0.2
HELM_VERSION=v2.15.2

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
\n        name: manager\
\n        args:\
\n        - --port=8443\
\n        - --logtostderr\
\n        - --emit-admission-events\
\n        - --exempt-namespace=gatekeeper-system\
\n        - --operation=webhook\
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
\n        name: auditcontainer\
\n        args:\
\n        - --emit-audit-events\
\n        - --operation=audit\
\n        - --operation=status\
\n        - --logtostderr"


FRAMEWORK_PACKAGE := github.com/open-policy-agent/frameworks/constraint

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= crd:trivialVersions=true

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: lint test manager

# Run tests
native-test: generate fmt vet manifests
	GO111MODULE=on go test -mod vendor ./pkg/... ./apis/... -coverprofile cover.out

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
	curl -L https://github.com/kubernetes-sigs/kind/releases/download/v${KIND_VERSION}/kind-linux-amd64 --output ${GITHUB_WORKSPACE}/bin/kind && chmod +x ${GITHUB_WORKSPACE}/bin/kind
	# Download and install kubectl
	curl -L https://storage.googleapis.com/kubernetes-release/release/$$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl -o ${GITHUB_WORKSPACE}/bin/kubectl && chmod +x ${GITHUB_WORKSPACE}/bin/kubectl
	# Download and install kustomize
	curl -L https://github.com/kubernetes-sigs/kustomize/releases/download/v${KUSTOMIZE_VERSION}/kustomize_${KUSTOMIZE_VERSION}_linux_amd64 -o ${GITHUB_WORKSPACE}/bin/kustomize && chmod +x ${GITHUB_WORKSPACE}/bin/kustomize
	# Download and install bats
	sudo apt-get -o Acquire::Retries=30 update && sudo apt-get -o Acquire::Retries=30 install -y bats
	# Check for existing kind cluster
	if [ $$(kind get clusters) ]; then kind delete cluster; fi
	# Create a new kind cluster
	TERM=dumb kind create cluster

e2e-build-load-image: docker-buildx
	kind load docker-image --name kind ${IMG}

e2e-verify-release: patch-image deploy test-e2e
	echo -e '\n\n======= manager logs =======\n\n' && kubectl logs -n gatekeeper-system -l control-plane=controller-manager

e2e-helm-deploy:
	# tiller needs enough permissions to create CRDs
	kubectl create clusterrolebinding tiller-admin --clusterrole=cluster-admin --serviceaccount=kube-system:default
	# Download and install helm
	rm -rf .staging/helm
	mkdir -p .staging/helm
	curl https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz > .staging/helm/helmbin.tar.gz
	cd .staging/helm && tar -xvf helmbin.tar.gz
	./.staging/helm/linux-amd64/helm init --wait --history-max=5
	kubectl -n kube-system wait --for=condition=Ready pod -l name=tiller --timeout=300s
	./.staging/helm/linux-amd64/helm install manifest_staging/charts/gatekeeper --name=tiger --set image.repository=${HELM_REPO} --set image.release=${HELM_RELEASE} --set emitAdmissionEvents=true --set emitAuditEvents=true

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
deploy: patch-image manifests
	kustomize build config/overlays/dev | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./apis/..." paths="./pkg/..." output:crd:artifacts:config=config/crd/bases
	rm -rf manifest_staging
	mkdir -p manifest_staging/deploy
	mkdir -p manifest_staging/charts/gatekeeper
	kustomize build config/default  -o manifest_staging/deploy/gatekeeper.yaml
	kustomize build cmd/build/helmify | go run cmd/build/helmify/*.go

# Run go fmt against code
fmt:
	GO111MODULE=on go fmt ./apis/... ./pkg/...
	GO111MODULE=on go fmt main.go

# Run go vet against code
vet:
	GO111MODULE=on go vet -mod vendor ./apis/... ./pkg/... ./third_party/...
	GO111MODULE=on go vet -mod vendor main.go

lint:
	golangci-lint -v run ./... --timeout 5m

# Generate code
generate: controller-gen target-template-source
	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths="./apis/..." paths="./pkg/..."

# Docker Login
docker-login:
	@docker login -u $(DOCKER_USER) -p $(DOCKER_PASSWORD) $(REGISTRY)

# Tag for Dev
docker-tag-dev:
	@docker tag $(IMG) $(REPOSITORY):$(DEV_TAG)
	@docker tag $(IMG) $(REPOSITORY):dev

# Tag for Dev
docker-tag-release:
	@docker tag $(IMG) $(REPOSITORY):$(VERSION)
	@docker tag $(IMG) $(REPOSITORY):latest

# Push for Dev
docker-push-dev:  docker-tag-dev
	@docker push $(REPOSITORY):$(DEV_TAG)
	@docker push $(REPOSITORY):dev

# Push for Release
docker-push-release:  docker-tag-release
	@docker push $(REPOSITORY):$(VERSION)
	@docker push $(REPOSITORY):latest

docker-build: test
	docker build --pull . -t ${IMG}

# Build docker image with buildx
# Experimental docker feature to build cross platform multi-architecture docker images
# https://docs.docker.com/buildx/working-with-buildx/
docker-buildx: test
	if ! DOCKER_CLI_EXPERIMENTAL=enabled docker buildx ls | grep -q container-builder; then\
		DOCKER_CLI_EXPERIMENTAL=enabled docker buildx create --name container-builder --use;\
	fi
	DOCKER_CLI_EXPERIMENTAL=enabled docker buildx build --platform "linux/amd64" \
		-t $(IMG) \
		. --load

docker-buildx-dev: test
	@if ! DOCKER_CLI_EXPERIMENTAL=enabled docker buildx ls | grep -q container-builder; then\
		DOCKER_CLI_EXPERIMENTAL=enabled docker buildx create --name container-builder --use;\
	fi
	DOCKER_CLI_EXPERIMENTAL=enabled docker buildx build --platform "linux/amd64,linux/arm64,linux/arm/v7" \
		-t $(REPOSITORY):$(DEV_TAG) \
		-t $(REPOSITORY):dev \
		. --push

docker-buildx-release: test
	@if ! DOCKER_CLI_EXPERIMENTAL=enabled docker buildx ls | grep -q container-builder; then\
		DOCKER_CLI_EXPERIMENTAL=enabled docker buildx create --name container-builder --use;\
	fi
	DOCKER_CLI_EXPERIMENTAL=enabled docker buildx build --platform "linux/amd64,linux/arm64,linux/arm/v7" \
		-t $(REPOSITORY):$(VERSION) \
		-t $(REPOSITORY):latest \
		. --push

# Update manager_image_patch.yaml with image tag
patch-image:
	@echo "updating kustomize image patch file for manager resource"
	@bash -c 'echo -e ${MANAGER_IMAGE_PATCH} > ./config/overlays/dev/manager_image_patch.yaml'
ifeq ($(USE_LOCAL_IMG),true)
	@sed -i '/^        name: manager/a \ \ \ \ \ \ \ \ imagePullPolicy: IfNotPresent' ./config/overlays/dev/manager_image_patch.yaml
	@sed -i '/^        name: auditcontainer/a \ \ \ \ \ \ \ \ imagePullPolicy: IfNotPresent' ./config/overlays/dev/manager_image_patch.yaml
endif
	@sed -i'' -e 's@image: .*@image: '"${IMG}"'@' ./config/overlays/dev/manager_image_patch.yaml

# Rebuild pkg/target/target_template_source.go to pull in pkg/target/regolib/src.rego
target-template-source:
	@printf "package target\n\n// This file is generated from pkg/target/regolib/src.rego via \"make target-template-source\"\n// Do not modify this file directly!\n\nconst templSrc = \`" > pkg/target/target_template_source.go
	@sed -e "s/data\[\"{{.DataRoot}}\"\]/{{.DataRoot}}/; s/data\[\"{{.ConstraintsRoot}}\"\]/{{.ConstraintsRoot}}/" pkg/target/regolib/src.rego >> pkg/target/target_template_source.go
	@printf "\`\n" >> pkg/target/target_template_source.go

# Push the docker image
docker-push:
	docker push ${IMG}

release-manifest:
	@sed -i -e 's/^VERSION := .*/VERSION := ${NEWVERSION}/' ./Makefile
	@sed -i'' -e 's@image: $(REPOSITORY):.*@image: $(REPOSITORY):'"$(NEWVERSION)"'@' ./config/manager/manager.yaml
	@sed -i "s/appVersion: .*/appVersion: ${NEWVERSION}/" ./cmd/build/helmify/static/Chart.yaml
	@sed -i "s/version: .*/version: ${NEWVERSION}/" ./cmd/build/helmify/static/Chart.yaml
	@sed -i "s/release: .*/release: ${NEWVERSION}/" ./cmd/build/helmify/static/values.yaml
	@sed -i 's/Current release version: `.*`/Current release version: `'"${NEWVERSION}"'`/' ./cmd/build/helmify/static/README.md
	@sed -i "s@repository: .*@repository: ${REPOSITORY}@" ./cmd/build/helmify/static/values.yaml
	export
	$(MAKE) manifests

promote-staging-manifest:
	@rm -rf deploy
	@cp -r manifest_staging/deploy .
	@rm -rf charts
	@cp -r manifest_staging/charts .
	@helm package ./charts/gatekeeper -d ./charts/gatekeeper
	@helm repo index ./charts/gatekeeper --url "https://raw.githubusercontent.com/open-policy-agent/gatekeeper/${NEWVERSION}/charts/gatekeeper/"

# Delete gatekeeper from a cluster. Note this is not a complete uninstall, just a dev convenience
uninstall:
	-kubectl delete -n gatekeeper-system Config config
	sleep 5
	kubectl delete ns gatekeeper-system

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	GO111MODULE=on go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.2.4
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

.PHONY: vendor
vendor:
	go mod vendor
