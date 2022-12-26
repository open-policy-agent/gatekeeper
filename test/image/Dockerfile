# Build the manager binary
FROM golang:1.19-bullseye as builder

ARG BATS_VERSION
ARG ORAS_VERSION
ARG YQ_VERSION
ARG KUSTOMIZE_VERSION
ARG KUBEBUILDER_VERSION
ARG TARGETARCH

RUN apt-get update &&\
    apt-get install -y apt-utils make

# Install kubebuilder
WORKDIR /scratch
RUN curl -L -O "https://github.com/kubernetes-sigs/kubebuilder/releases/download/v${KUBEBUILDER_VERSION}/kubebuilder_linux_${TARGETARCH}" &&\
    mv kubebuilder_linux_${TARGETARCH} /usr/local/kubebuilder
ENV PATH=$PATH:/usr/local/kubebuilder/bin:/usr/bin

# Install kustomize
RUN curl -L -O "https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv${KUSTOMIZE_VERSION}/kustomize_v${KUSTOMIZE_VERSION}_linux_${TARGETARCH}.tar.gz" &&\
    tar -zxvf kustomize_v${KUSTOMIZE_VERSION}_linux_${TARGETARCH}.tar.gz &&\
    chmod +x kustomize &&\
    mv kustomize /usr/local/bin

# Install bats
RUN curl -sSLO https://github.com/bats-core/bats-core/archive/v${BATS_VERSION}.tar.gz && \
    tar -zxvf v${BATS_VERSION}.tar.gz && \
    bash bats-core-${BATS_VERSION}/install.sh /usr/local

# Install ORAS
RUN curl -SsLO https://github.com/oras-project/oras/releases/download/v${ORAS_VERSION}/oras_${ORAS_VERSION}_linux_amd64.tar.gz && \
    mkdir -p oras-install/ && tar -zxf oras_${ORAS_VERSION}_*.tar.gz -C oras-install/ && \
    mv oras-install/oras /usr/local/bin/ && rm -rf oras_${ORAS_VERSION}_*.tar.gz oras-install/

# Install yq and jq
RUN curl -LsS https://github.com/mikefarah/yq/releases/download/v${YQ_VERSION}/yq_linux_amd64 -o /usr/local/bin/yq \
    && chmod +x /usr/local/bin/yq
RUN apt-get update && yes | apt-get install jq

# Install docker
RUN curl -fsSL https://get.docker.com | sh

WORKDIR /app
