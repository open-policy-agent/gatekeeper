FROM golang:1.20-bullseye@sha256:2ae255c69b2e96cac149ca10b02b9fd3ef71033e0187fb8c9c3c333626e5ae50

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
