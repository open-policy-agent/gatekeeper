ARG BUILDPLATFORM="linux/amd64"
ARG BUILDERIMAGE="golang:1.15-alpine"
# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
ARG BASEIMAGE="gcr.io/distroless/static:nonroot"

FROM --platform=$BUILDPLATFORM $BUILDERIMAGE as builder

ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG LDFLAGS

ENV GO111MODULE=on\
    CGO_ENABLED=0

WORKDIR /go/src/github.com/open-policy-agent/gatekeeper

COPY pkg/ pkg/
COPY third_party/ third_party/
COPY vendor/ vendor/
COPY main.go main.go
COPY apis/ apis/
COPY go.mod .

RUN export GOOS=$TARGETOS && \
    export GOARCH=$TARGETARCH && \
    GOARM=$(echo ${TARGETPLATFORM} | cut -d / -f3); export GOARM=${GOARM:1} && \
    go build -mod vendor -a -ldflags "${LDFLAGS:--X github.com/open-policy-agent/gatekeeper/pkg/version.Version=latest}" -o manager main.go

FROM $BASEIMAGE

WORKDIR /

COPY --from=builder /go/src/github.com/open-policy-agent/gatekeeper/manager .

USER nonroot:nonroot

ENTRYPOINT ["/manager"]
