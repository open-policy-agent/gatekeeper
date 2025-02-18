FROM golang:1.24-bookworm@sha256:6260304a09fb81a1983db97c9e6bfc1779ebce33d39581979a511b3c7991f076

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
