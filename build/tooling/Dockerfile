FROM golang:1.21-bullseye@sha256:23ad9fe7915fab922c85c8ab34768c5fb58f10c20fdcce3c5b700cbffdb2ae78

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
