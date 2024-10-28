FROM --platform=$BUILDPLATFORM golang:1.23-bookworm@sha256:1a5326b07cbab12f4fd7800425f2cf25ff2bd62c404ef41b56cb99669a710a83 as builder

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

FROM gcr.io/distroless/static-debian12@sha256:cc226ca14d17d01d4b278d9489da930a0dd11150df10ae95829d13e6d00fbdbf

WORKDIR /

COPY --from=builder /go/src/github.com/open-policy-agent/gatekeeper/test/externaldata/dummy-provider/provider .

COPY --from=builder --chown=65532:65532 /go/src/github.com/open-policy-agent/gatekeeper/test/externaldata/dummy-provider/certs/server.crt \
    /go/src/github.com/open-policy-agent/gatekeeper/test/externaldata/dummy-provider/certs/server.key \
    /etc/ssl/certs/

USER 65532:65532

ENTRYPOINT ["/provider"]
