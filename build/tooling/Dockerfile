FROM golang:1.20-bullseye@sha256:a3598b93d32819f1759893c532fa186bc61d58f1ced9aa49c2c77fe13383159a

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
