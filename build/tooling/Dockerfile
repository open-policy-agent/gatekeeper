FROM golang:1.26-trixie@sha256:4ee9ffa999b4583ce281939cdff828763083610292f252279a0cee77473bd9a7

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
