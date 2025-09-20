FROM golang:1.25-trixie@sha256:c8c8d55e02aa456e99a4fd0d2a224c0fb487d609c86a2550e8d64c3021b1df9a

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
