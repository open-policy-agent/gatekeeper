FROM golang:1.26-trixie@sha256:0dcba0d95dbfb072e9917a106b9e07d7cc298097dc83e9307056ef1889de654d

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
