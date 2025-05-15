FROM golang:1.24-bookworm@sha256:89a04cc2e2fbafef82d4a45523d4d4ae4ecaf11a197689036df35fef3bde444a

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
