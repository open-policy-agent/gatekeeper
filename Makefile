
# Image URL to use all building/pushing image targets
REGISTRY ?= quay.io
REPOSITORY ?= $(REGISTRY)/open-policy-agent/gatekeeper

IMG := $(REPOSITORY):latest

VERSION := v3.0.2

BUILD_COMMIT := $(shell ./build/get-build-commit.sh)
BUILD_TIMESTAMP := $(shell ./build/get-build-timestamp.sh)
BUILD_HOSTNAME := $(shell ./build/get-build-hostname.sh)

LDFLAGS := "-X github.com/open-policy-agent/gatekeeper/version.Version=$(VERSION) \
	-X github.com/open-policy-agent/gatekeeper/version.Vcs=$(BUILD_COMMIT) \
	-X github.com/open-policy-agent/gatekeeper/version.Timestamp=$(BUILD_TIMESTAMP) \
	-X github.com/open-policy-agent/gatekeeper/version.Hostname=$(BUILD_HOSTNAME)"

MANAGER_IMAGE_PATCH := "apiVersion: apps/v1\
\nkind: StatefulSet\
\nmetadata:\
\n  name: controller-manager\
\n  namespace: system\
\nspec:\
\n  template:\
\n    spec:\
\n      containers:\
\n      - image: <your image file>\
\n        name: manager"

all: test manager

# Run tests
native-test: generate fmt vet manifests
	go test ./pkg/... ./cmd/... -coverprofile cover.out

# Hook to run docker tests
.PHONY: test
test:
	rm -rf .staging/test
	mkdir -p .staging/test
	cp -r * .staging/test
	-rm .staging/test/Dockerfile
	cp test/Dockerfile .staging/test/Dockerfile
	docker build .staging/test -t gatekeeper-test && docker run -t gatekeeper-test

# Build manager binary
manager: generate fmt vet
	go build -o bin/manager  -ldflags $(LDFLAGS) github.com/open-policy-agent/gatekeeper/cmd/manager

# Build manager binary
manager-osx: generate fmt vet
	go build -o bin/manager GOOS=darwin  -ldflags $(LDFLAGS) github.com/open-policy-agent/gatekeeper/cmd/manager

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet
	go run ./cmd/manager/main.go

# Install CRDs into a cluster
install: manifests
	kubectl apply -f config/crds

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests
	touch -a ./config/manager_image_patch.yaml
	kubectl apply -f config/crds
	kubectl apply -f vendor/github.com/open-policy-agent/frameworks/constraint/config/crds
	kustomize build config | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests:
	go run vendor/sigs.k8s.io/controller-tools/cmd/controller-gen/main.go all

# Run go fmt against code
fmt:
	go fmt ./pkg/... ./cmd/...

# Run go vet against code
vet:
	go vet ./pkg/... ./cmd/...

# Generate code
generate:
	go generate ./pkg/... ./cmd/...

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
docker-build:
	docker build . -t ${IMG}
	@echo "updating kustomize image patch file for manager resource"

	@test -s ./config/manager_image_patch.yaml || bash -c 'echo -e ${MANAGER_IMAGE_PATCH} > ./config/manager_image_patch.yaml'

	@sed -i'' -e 's@image: .*@image: '"${IMG}"'@' ./config/manager_image_patch.yaml

docker-build-ci:
	docker build . -t $(IMG) -f Dockerfile_ci

# Push the docker image
docker-push:
	docker push ${IMG}

# Travis Dev Deployment
travis-dev-deploy: docker-login docker-build-ci docker-push-dev

# Travis Release
travis-dev-release: docker-login docker-build-ci docker-push-release

# Delete gatekeeper from a cluster. Note this is not a complete uninstall, just a dev convenience
uninstall:
	-kubectl delete -n gatekeeper-system Config config
	sleep 5
	kubectl delete ns gatekeeper-system
