FROM golang:1.25-trixie@sha256:27e1c927a07ed2c7295d39941d6d881424739dbde9ae3055d0d3013699ed35e8

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
