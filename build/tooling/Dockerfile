FROM golang:1.22-bookworm@sha256:39b7e6ebaca464d51989858871f792f2e186dce8ce0cbdba7e88e4444b244407

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
