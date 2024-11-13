FROM --platform=$BUILDPLATFORM golang:1.23-bookworm@sha256:0e3377d7a71c1fcb31cdc3215292712e83baec44e4792aeaa75e503cfcae16ec AS builder

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

FROM --platform=$BUILDPLATFORM gcr.io/distroless/static-debian12@sha256:f4a57e8ffd7ba407bdd0eb315bb54ef1f21a2100a7f032e9102e4da34fe7c196 AS build
USER 65532:65532
COPY --from=builder --chown=65532:65532 /gator /gator
ENTRYPOINT ["/gator"]
