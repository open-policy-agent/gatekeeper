FROM --platform=$BUILDPLATFORM golang:1.24-bookworm@sha256:fa1a01d362a7b9df68b021d59a124d28cae6d99ebd1a876e3557c4dd092f1b1d AS builder

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

COPY . /go/src/github.com/open-policy-agent/gatekeeper
WORKDIR /go/src/github.com/open-policy-agent/gatekeeper/cmd/gator

RUN go build -mod vendor -a -ldflags "${LDFLAGS}" -o /gator

FROM --platform=$BUILDPLATFORM gcr.io/distroless/static-debian12@sha256:3d0f463de06b7ddff27684ec3bfd0b54a425149d0f8685308b1fdf297b0265e9 AS build
USER 65532:65532
COPY --from=builder --chown=65532:65532 /gator /gator
ENTRYPOINT ["/gator"]
