# Build the manager binary
FROM golang:1.13.3 as builder

# Copy in the go src
WORKDIR /go/src/github.com/open-policy-agent/gatekeeper
COPY pkg/    pkg/
COPY vendor/ vendor/
COPY main.go main.go
COPY api/ api/
COPY go.mod .

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod vendor -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /go/src/github.com/open-policy-agent/gatekeeper/manager .
USER nonroot:nonroot

ENTRYPOINT ["/manager"]
