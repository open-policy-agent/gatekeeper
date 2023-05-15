FROM golang:1.20-bullseye@sha256:595c9af0430dd84bad33020e7e9e328af4bd1a1aabd46a03b5bf6f252cdbecf3

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
