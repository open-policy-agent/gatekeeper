FROM --platform=$BUILDPLATFORM golang:1.24-bookworm@sha256:d7d795d0a9f51b00d9c9bfd17388c2c626004a50c6ed7c581e095122507fe1ab as builder

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

RUN go mod init && go mod tidy

RUN go build -o provider provider.go

FROM gcr.io/distroless/static-debian12@sha256:3f2b64ef97bd285e36132c684e6b2ae8f2723293d09aae046196cca64251acac

WORKDIR /

COPY --from=builder /go/src/github.com/open-policy-agent/gatekeeper/test/externaldata/dummy-provider/provider .

COPY --from=builder --chown=65532:65532 /go/src/github.com/open-policy-agent/gatekeeper/test/externaldata/dummy-provider/certs/server.crt \
    /go/src/github.com/open-policy-agent/gatekeeper/test/externaldata/dummy-provider/certs/server.key \
    /etc/ssl/certs/

USER 65532:65532

ENTRYPOINT ["/provider"]
