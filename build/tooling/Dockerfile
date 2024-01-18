FROM golang:1.21-bullseye@sha256:6ac4c35eed9933eb7ae528e9549182bcf421c7f0e39b7337411a54c2f6a8b680

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
