FROM golang:1.23-bookworm

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.5
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.31.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
