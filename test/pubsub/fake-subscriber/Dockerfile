ARG BUILDPLATFORM="linux/amd64"
ARG BUILDERIMAGE="golang:1.22-bookworm"
ARG BASEIMAGE="gcr.io/distroless/static-debian12:nonroot"

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

WORKDIR /go/src/github.com/open-policy-agent/gatekeeper/test/pubsub/fake-subscriber

COPY . .

RUN go mod init && go mod tidy && go mod vendor

RUN go build -o main

FROM $BASEIMAGE

WORKDIR /

COPY --from=builder /go/src/github.com/open-policy-agent/gatekeeper/test/pubsub/fake-subscriber/main .

USER 65532:65532

ENTRYPOINT ["/main"]
