FROM golang:1.24-bookworm@sha256:b970e6d47c09fdd34179acef5c4fecaf6410f0b597a759733b3cbea04b4e604a as builder

ARG BATS_VERSION
ARG ORAS_VERSION
ARG YQ_VERSION
ARG KUSTOMIZE_VERSION

ARG TARGETARCH

RUN git config --global --add safe.directory "*"

RUN apt-get update && \
    apt-get install -y make jq apt-utils

# Install kustomize
RUN curl -L -O "https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv${KUSTOMIZE_VERSION}/kustomize_v${KUSTOMIZE_VERSION}_linux_${TARGETARCH}.tar.gz" &&\
    tar -zxvf kustomize_v${KUSTOMIZE_VERSION}_linux_${TARGETARCH}.tar.gz &&\
    chmod +x kustomize &&\
    mv kustomize /usr/local/bin

# Install bats
RUN curl -sSLO https://github.com/bats-core/bats-core/archive/v${BATS_VERSION}.tar.gz && \
    tar -zxvf v${BATS_VERSION}.tar.gz && \
    bash bats-core-${BATS_VERSION}/install.sh /usr/local

# Install oras
RUN curl -SsLO https://github.com/oras-project/oras/releases/download/v${ORAS_VERSION}/oras_${ORAS_VERSION}_linux_${TARGETARCH}.tar.gz && \
    mkdir -p oras-install/ && tar -zxf oras_${ORAS_VERSION}_*.tar.gz -C oras-install/ && \
    mv oras-install/oras /usr/local/bin/ && rm -rf oras_${ORAS_VERSION}_*.tar.gz oras-install/

# Install yq
RUN curl -LsS https://github.com/mikefarah/yq/releases/download/v${YQ_VERSION}/yq_linux_${TARGETARCH} -o /usr/local/bin/yq \
    && chmod +x /usr/local/bin/yq

# Install docker
RUN curl -fsSL https://get.docker.com | sh

WORKDIR /app
