FROM --platform=$BUILDPLATFORM golang:1.25-trixie@sha256:c8c8d55e02aa456e99a4fd0d2a224c0fb487d609c86a2550e8d64c3021b1df9a AS builder

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

WORKDIR /go/src/github.com/open-policy-agent/gatekeeper/test/export/fake-reader

COPY . .

RUN go mod init && go mod tidy && go mod vendor

RUN go build -o main

FROM gcr.io/distroless/static-debian12@sha256:87bce11be0af225e4ca761c40babb06d6d559f5767fbf7dc3c47f0f1a466b92c

WORKDIR /

COPY --from=builder /go/src/github.com/open-policy-agent/gatekeeper/test/export/fake-reader/main .

USER 65532:65532

ENTRYPOINT ["/main"]