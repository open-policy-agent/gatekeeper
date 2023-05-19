FROM golang:1.20-bullseye@sha256:2dc5c568c8a314583090e887e8d96d313e081e2c5333d0a7b935906baf77cee9

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
