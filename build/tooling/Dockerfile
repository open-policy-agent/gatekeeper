FROM golang:1.25-bookworm@sha256:6ad9415c04f1cdb7888141cb247cabb28fa74fcc597034494c65d5bc783246a0

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
