# Build the manager binary
FROM golang:1.12.9 as builder

RUN apt-get update && apt-get install -y apt-utils
RUN apt-get install -y make

# Install kubebuilder
WORKDIR /scratch
ENV version=1.0.8
ENV arch=amd64
RUN curl -L -O "https://github.com/kubernetes-sigs/kubebuilder/releases/download/v${version}/kubebuilder_${version}_linux_${arch}.tar.gz"
RUN tar -zxvf kubebuilder_${version}_linux_${arch}.tar.gz
RUN mv kubebuilder_${version}_linux_${arch} kubebuilder && mv kubebuilder /usr/local/
ENV PATH=$PATH:/usr/local/kubebuilder/bin:/usr/bin

# Install kustomize
ENV version=3.0.2
ENV arch=amd64
RUN curl -L -O "https://github.com/kubernetes-sigs/kustomize/releases/download/v${version}/kustomize_${version}_linux_${arch}"
RUN mv kustomize_${version}_linux_${arch} /usr/bin/kustomize
RUN chmod u+x /usr/bin/kustomize

# Copy in the go src
WORKDIR /go/src/github.com/open-policy-agent/gatekeeper
COPY .    .

ENTRYPOINT ["make", "native-test"]
