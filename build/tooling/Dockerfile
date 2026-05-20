FROM golang:1.26-trixie@sha256:a085df697019cb63b40a70f6a92b948f7dc9df96dfcb2c20ba6eed25ce28f5b3

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
