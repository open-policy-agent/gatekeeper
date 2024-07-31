FROM golang:1.22-bookworm@sha256:af9b40f2b1851be993763b85288f8434af87b5678af04355b1e33ff530b5765f

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
