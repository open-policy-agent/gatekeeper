FROM golang:1.21-bullseye@sha256:c62751ac12cad0c514d941e36f846c1c440ca9e8ec08dd87d022fb03f0887a9b

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.10.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.25.4

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
