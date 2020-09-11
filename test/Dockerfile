# Build the manager binary
FROM golang:1.15 as builder

RUN apt-get update &&\
    apt-get install -y apt-utils make

# Install kubebuilder
WORKDIR /scratch
ENV version=2.3.1
ENV arch=amd64
RUN curl -L -O "https://github.com/kubernetes-sigs/kubebuilder/releases/download/v${version}/kubebuilder_${version}_linux_${arch}.tar.gz" &&\
    tar -zxvf kubebuilder_${version}_linux_${arch}.tar.gz &&\
    mv kubebuilder_${version}_linux_${arch} /usr/local/kubebuilder &&\
    rm kubebuilder_${version}_linux_${arch}.tar.gz
ENV PATH=$PATH:/usr/local/kubebuilder/bin:/usr/bin

# Install kustomize
ENV version=3.7.0
ENV arch=amd64
RUN curl -L -O "https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv${version}/kustomize_v${version}_linux_${arch}.tar.gz" &&\
    tar -zxvf kustomize_v${version}_linux_${arch}.tar.gz &&\
    chmod +x kustomize &&\
    mv kustomize /usr/local/bin

# Copy in the go src
WORKDIR /go/src/github.com/open-policy-agent/gatekeeper
COPY .    .

ENTRYPOINT ["make", "native-test"]
