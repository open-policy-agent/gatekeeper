FROM golang:1.20-bullseye@sha256:d9f75197f484a4d7e008a0a8f7d1e68344ea1c1ba030273d5ab1567e129eed55

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
