FROM --platform=$TARGETPLATFORM registry.k8s.io/kubectl:v1.30.1 as builder

ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH

FROM scratch as build
USER 65532:65532
COPY --chown=65532:65532 * /crds/
COPY --from=builder /bin/kubectl /kubectl
ENTRYPOINT ["/kubectl"]
