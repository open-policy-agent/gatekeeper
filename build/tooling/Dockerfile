FROM golang:1.21-bullseye@sha256:adf7ccb07fe8ccadf7bb0317f02d2c3a4916f824a23f6975fd36c4bd7feece3f

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
