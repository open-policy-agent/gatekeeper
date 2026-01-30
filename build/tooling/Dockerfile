FROM golang:1.25-trixie@sha256:fb4b74a39c7318d53539ebda43ccd3ecba6e447a78591889c0efc0a7235ea8b3

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
