FROM golang:1.20-bullseye@sha256:74b09b3b6fa5aa542df8ef974cb745eb477be72f6fcf821517fb410aff532b00

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
