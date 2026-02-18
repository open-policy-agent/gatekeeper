FROM golang:1.26-trixie@sha256:889885d7cc1275935e3f9920aabadc5fadbe873f633d92a746f1bc401dd40f69

RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0
RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@v0.29.3

RUN mkdir /gatekeeper
WORKDIR /gatekeeper
