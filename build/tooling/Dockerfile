FROM golang:1.20-bullseye@sha256:918857f4064db0fff49799ce5e7c4d43e394f452111cd89cca9af539c18a76a8

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
