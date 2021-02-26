FROM golang:1.16

RUN GO111MODULE=on go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.3.0

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
