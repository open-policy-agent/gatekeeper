FROM alpine as builder
ARG KUBE_VERSION=v1.21.2
ARG ARCH=amd64

RUN apk add --no-cache curl && \
    curl -LO https://storage.googleapis.com/kubernetes-release/release/${KUBE_VERSION}/bin/linux/${ARCH}/kubectl && \
    chmod +x kubectl

FROM gcr.io/distroless/static
COPY * /crds/
COPY --from=builder /kubectl /kubectl
ENTRYPOINT ["/kubectl"]
