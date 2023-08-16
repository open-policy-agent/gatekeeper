FROM golang:1.21-bullseye@sha256:0ed263861f71db3c4fed4d900b621aca03a3b91b8bc5f1dd56584af191c89d68

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
