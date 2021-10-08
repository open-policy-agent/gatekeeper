ARG BUILDPLATFORM="linux/amd64"
ARG BUILDERIMAGE="golang:1.17"
ARG BASEIMAGE="gcr.io/distroless/static:nonroot-amd64"

FROM --platform=$BUILDPLATFORM $BUILDERIMAGE as builder

ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT=""
ARG LDFLAGS

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    GOARM=${TARGETVARIANT}

WORKDIR /go/src/github.com/open-policy-agent/gatekeeper/test/externaldata/dummy-provider

COPY . .

RUN go build -mod vendor -o provider main.go

FROM $BASEIMAGE

WORKDIR /

COPY --from=builder /go/src/github.com/open-policy-agent/gatekeeper/test/externaldata/dummy-provider .

USER 65532:65532

ENTRYPOINT ["/provider"]
