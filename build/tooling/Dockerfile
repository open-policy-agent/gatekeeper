FROM golang:1.21-bullseye@sha256:47fa179d4966a0950485ede2ef81567bb1cf62e1e87af07e9830e5c928d06cd0

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
