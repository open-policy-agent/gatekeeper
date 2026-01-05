FROM golang:1.25-trixie@sha256:ef151f0384896831258e71065176f1e63f5a90bcbe6a98ec679a1990011a2655

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
