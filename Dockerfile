ARG BUILDPLATFORM="linux/amd64"
FROM --platform=$BUILDPLATFORM golang:1.14-alpine as builder

ARG TARGETPLATFORM

ENV GO111MODULE=on\
    CGO_ENABLED=0

WORKDIR /go/src/github.com/open-policy-agent/gatekeeper

COPY pkg/ pkg/
COPY third_party/ third_party/
COPY vendor/ vendor/
COPY main.go main.go
COPY apis/ apis/
COPY go.mod .

RUN export GOOS=$(echo ${TARGETPLATFORM} | cut -d / -f1) && \
    export GOARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) && \
    GOARM=$(echo ${TARGETPLATFORM} | cut -d / -f3); export GOARM=${GOARM:1} && \
    go build -mod vendor -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /go/src/github.com/open-policy-agent/gatekeeper/manager .

USER nonroot:nonroot

ENTRYPOINT ["/manager"]
