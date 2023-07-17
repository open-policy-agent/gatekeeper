FROM golang:1.20-bullseye@sha256:851af0a8ca4eba552c84db5b2edac7f3be15deb5892217961a1d4b175585a603

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
