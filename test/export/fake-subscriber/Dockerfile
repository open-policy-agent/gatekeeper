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

FROM gcr.io/distroless/static-debian12@sha256:cd64bec9cec257044ce3a8dd3620cf83b387920100332f2b041f19c4d2febf93

WORKDIR /

COPY --from=builder /go/src/github.com/open-policy-agent/gatekeeper/test/export/fake-subscriber/main .

USER 65532:65532

ENTRYPOINT ["/main"]
