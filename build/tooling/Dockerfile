FROM golang:1.25-trixie@sha256:8e8f9c84609b6005af0a4a8227cee53d6226aab1c6dcb22daf5aeeb8b05480e1

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
