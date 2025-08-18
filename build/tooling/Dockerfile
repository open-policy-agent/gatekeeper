FROM golang:1.25-bookworm@sha256:74908ad827a5849c557eeca81d46263acf788ead606102d83466f499f83e35b1

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
