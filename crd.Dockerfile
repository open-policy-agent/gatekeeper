FROM alpine as builder

ARG TARGETOS
ARG TARGETARCH
ARG KUBE_VERSION

RUN apk add --no-cache curl && \
    curl -LO https://storage.googleapis.com/kubernetes-release/release/v${KUBE_VERSION}/bin/${TARGETOS}/${TARGETARCH}/kubectl && \
    chmod +x kubectl

FROM scratch
COPY * /crds/
COPY --from=builder /kubectl /kubectl
ENTRYPOINT ["/kubectl"]
