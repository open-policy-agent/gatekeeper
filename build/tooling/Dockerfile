FROM golang:1.22-bookworm@sha256:6c2780255bb7b881e904e303be0d7a079054160b2ce1efde446693c0850a39ad

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
