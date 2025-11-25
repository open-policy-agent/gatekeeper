FROM golang:1.25-trixie@sha256:a02d35efc036053fdf0da8c15919276bf777a80cbfda6a35c5e9f087e652adfc

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
