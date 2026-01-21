FROM --platform=$BUILDPLATFORM golang:1.25-trixie@sha256:fb4b74a39c7318d53539ebda43ccd3ecba6e447a78591889c0efc0a7235ea8b3 AS builder

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

RUN \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -a -ldflags "${LDFLAGS}" -o /gator

FROM --platform=$BUILDPLATFORM gcr.io/distroless/static-debian12@sha256:cd64bec9cec257044ce3a8dd3620cf83b387920100332f2b041f19c4d2febf93 AS build
USER 65532:65532
COPY --from=builder --chown=65532:65532 /gator /gator
ENTRYPOINT ["/gator"]
