FROM golang:1.21-bullseye@sha256:1e3c713a9f5419786d85d0feb343ceff119d0f82f7ab0fefffa4150420c3ad7f

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
