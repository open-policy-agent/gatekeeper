FROM --platform=$BUILDPLATFORM golang:1.25-trixie@sha256:7534a6264850325fcce93e47b87a0e3fddd96b308440245e6ab1325fa8a44c91 AS builder

ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT=""
ARG LDFLAGS
ARG BUILDKIT_SBOM_SCAN_STAGE=true

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    GOARM=${TARGETVARIANT}

WORKDIR /go/src/github.com/open-policy-agent/gatekeeper
COPY . .

RUN go build -mod vendor -a -ldflags "${LDFLAGS}" -o manager

FROM gcr.io/distroless/static-debian12@sha256:87bce11be0af225e4ca761c40babb06d6d559f5767fbf7dc3c47f0f1a466b92c

WORKDIR /
COPY --from=builder /go/src/github.com/open-policy-agent/gatekeeper/manager .
USER 65532:65532
ENTRYPOINT ["/manager"]


FROM --platform=$BUILDPLATFORM golang:1.25-trixie@sha256:7534a6264850325fcce93e47b87a0e3fddd96b308440245e6ab1325fa8a44c91 AS builder

ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT=""
ARG LDFLAGS
ARG BUILDKIT_SBOM_SCAN_STAGE=true

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    GOARM=${TARGETVARIANT}

WORKDIR /go/src/github.com/open-policy-agent/gatekeeper

# Install kube-api-linter for CI/linting purposes.
# This tool checks Kubernetes YAML files against best practices.
# The version is pinned to ensure reproducible CI builds.
# An external CI workflow is required to execute the linter.
RUN go install github.com/kubernetes-sigs/kube-api-linter/cmd/kube-api-linter@v1.6.0

COPY . .

RUN go build -mod vendor -a -ldflags "${LDFLAGS}" -o manager

FROM gcr.io/distroless/static-debian12@sha256:87bce11be0af225e4ca761c40babb06d6d559f5767fbf7dc3c47f0f1a466b92c

WORKDIR /
COPY --from=builder /go/src/github.com/open-policy-agent/gatekeeper/manager .
USER 65532:65532
ENTRYPOINT ["/manager"]