FROM --platform=$BUILDPLATFORM golang:1.25-trixie@sha256:dfdd969010ba978942302cee078235da13aef030d22841e873545001d68a61a7 AS builder

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

WORKDIR /go/src/github.com/open-policy-agent/gatekeeper/test/export/fake-subscriber

COPY . .

RUN go mod init && go mod tidy && go mod vendor

RUN go build -o main

FROM gcr.io/distroless/static-debian12@sha256:20bc6c0bc4d625a22a8fde3e55f6515709b32055ef8fb9cfbddaa06d1760f838

WORKDIR /

COPY --from=builder /go/src/github.com/open-policy-agent/gatekeeper/test/export/fake-subscriber/main .

USER 65532:65532

ENTRYPOINT ["/main"]
