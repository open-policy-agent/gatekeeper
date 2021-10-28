FROM golang:1.17

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.5.0

RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@release-1.22

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
