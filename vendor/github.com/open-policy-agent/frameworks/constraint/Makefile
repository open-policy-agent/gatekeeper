all: test

# Run tests
native-test: generate fmt vet manifests
	go test ./pkg/... -coverprofile cover.out

# Docker internal test
docker-internal-test:
	make native-test
# The remote driver has some unimplemented functions
# which result in a panic during testing.
#	./tests/test_remote_driver.sh

# Hook to run docker tests
test:
	docker build . -t constraint-test && docker run -t constraint-test

# Install CRDs into a cluster
install: manifests
	kubectl apply -f config/crds

# Generate manifests e.g. CRD, RBAC etc.
manifests:
	go run vendor/sigs.k8s.io/controller-tools/cmd/controller-gen/main.go all
	kustomize build config -o deploy/crds.yaml

# Run go fmt against code
fmt:
	go fmt ./pkg/...

# Run go vet against code
vet:
	go vet ./pkg/...

# Generate code
generate:
ifndef GOPATH
	$(error GOPATH not defined, please define GOPATH. Run "go help gopath" to learn more about GOPATH)
endif
	go generate ./pkg/...
