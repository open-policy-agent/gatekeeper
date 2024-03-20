FROM golang:1.22-bullseye@sha256:84825c0ec01d277311cfbad14321338166fc7ffa73202db3a2debfeb89e8ddd5

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
