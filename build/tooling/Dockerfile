FROM golang:1.25-trixie@sha256:61226c61f37cb86253c4ac486ef22c47f14bfddb8f60bb4805bfc165001be758

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
