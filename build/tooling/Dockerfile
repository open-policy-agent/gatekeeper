FROM golang:1.24-bookworm@sha256:75e6700eab3c994f730e36f357a26ee496b618d51eaecb04716144e861ad74f3

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
