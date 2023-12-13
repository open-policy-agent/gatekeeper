FROM golang:1.21-bullseye@sha256:fe69f483d2ef3e2309ecb19d18ab01054711746bc0a31bf5fbcffccb32182f05

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
