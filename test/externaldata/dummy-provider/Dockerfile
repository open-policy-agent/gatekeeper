FROM --platform=$BUILDPLATFORM golang:1.25-bookworm@sha256:c4bc0741e3c79c0e2d47ca2505a06f5f2a44682ada94e1dba251a3854e60c2bd AS builder

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

FROM gcr.io/distroless/static-debian12@sha256:87bce11be0af225e4ca761c40babb06d6d559f5767fbf7dc3c47f0f1a466b92c

WORKDIR /

COPY --from=builder /go/src/github.com/open-policy-agent/gatekeeper/test/externaldata/dummy-provider/provider .

COPY --from=builder --chown=65532:65532 /go/src/github.com/open-policy-agent/gatekeeper/test/externaldata/dummy-provider/certs/server.crt \
    /go/src/github.com/open-policy-agent/gatekeeper/test/externaldata/dummy-provider/certs/server.key \
    /etc/ssl/certs/

USER 65532:65532

ENTRYPOINT ["/provider"]
