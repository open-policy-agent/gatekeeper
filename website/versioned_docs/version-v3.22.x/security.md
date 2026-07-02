---
id: security
title: Security
---

Please report vulnerabilities by email to [open-policy-agent-security](mailto:open-policy-agent-security@googlegroups.com).
We will send a confirmation message to acknowledge that we have received the
report and then we will send additional messages to follow up once the issue
has been investigated.

For details on the security release process please refer to the [open-policy-agent/opa/SECURITY.md](https://github.com/open-policy-agent/opa/blob/main/SECURITY.md) file.

# Build Attestations

Gatekeeper provides build attestations for each release starting with v3.12.0. These attestations describe the image contents and how they were built. They are generated using [Docker BuildKit](https://docs.docker.com/build/buildkit/) v0.11 or later. To get more information about build attestations, please refer to the [Docker build attestations documentation](https://docs.docker.com/build/attestations/).

Gatekeeper provides [Software Bill of Materials (SBOM)](https://docs.docker.com/build/attestations/sbom/) and [SLSA Provenance](https://docs.docker.com/build/attestations/slsa-provenance/) for each image.

To get a list of images per OS and architecture and their corresponding attestations, please run:

```shell
$ docker buildx imagetools inspect openpolicyagent/gatekeeper:v3.12.0-rc.0
Name:      docker.io/openpolicyagent/gatekeeper:v3.12.0-rc.0
MediaType: application/vnd.oci.image.index.v1+json
Digest:    sha256:64b920b4b6d585d097649001e3a1794ae7669603f7e23b6af9156f67b21a6227

Manifests:
  Name:        docker.io/openpolicyagent/gatekeeper:v3.12.0-rc.0@sha256:459c6662ed72bae083b7ba0da49037009dc10cee23e60a8d144df8c1663487a5
  MediaType:   application/vnd.oci.image.manifest.v1+json
  Platform:    linux/amd64

  Name:        docker.io/openpolicyagent/gatekeeper:v3.12.0-rc.0@sha256:53aeec87b4c5c7ced14c66e923728da4f321b85ebb14b4b30c2636d63946f714
  MediaType:   application/vnd.oci.image.manifest.v1+json
  Platform:    linux/arm64

  Name:        docker.io/openpolicyagent/gatekeeper:v3.12.0-rc.0@sha256:bc97e9f352d90961da6889534d01d1a955f348397ade55da035e2be127d13688
  MediaType:   application/vnd.oci.image.manifest.v1+json
  Platform:    linux/arm/v7

  Name:        docker.io/openpolicyagent/gatekeeper:v3.12.0-rc.0@sha256:f92564f87778c93070f9988f33723b5d7ce3d92afdbd2b959be8d8df190a9026
  MediaType:   application/vnd.oci.image.manifest.v1+json
  Platform:    unknown/unknown
  Annotations:
    vnd.docker.reference.digest: sha256:459c6662ed72bae083b7ba0da49037009dc10cee23e60a8d144df8c1663487a5
    vnd.docker.reference.type:   attestation-manifest

  Name:        docker.io/openpolicyagent/gatekeeper:v3.12.0-rc.0@sha256:509672047e55607cc729ee29d96e1dee5d3fbeb75770e7ce11ddbbc60e0ed527
  MediaType:   application/vnd.oci.image.manifest.v1+json
  Platform:    unknown/unknown
  Annotations:
    vnd.docker.reference.digest: sha256:53aeec87b4c5c7ced14c66e923728da4f321b85ebb14b4b30c2636d63946f714
    vnd.docker.reference.type:   attestation-manifest

  Name:        docker.io/openpolicyagent/gatekeeper:v3.12.0-rc.0@sha256:d65af6b76cbef07ad9e4d054b1a7b9586c0f4f732701781401d71f1a60bd626d
  MediaType:   application/vnd.oci.image.manifest.v1+json
  Platform:    unknown/unknown
  Annotations:
    vnd.docker.reference.digest: sha256:bc97e9f352d90961da6889534d01d1a955f348397ade55da035e2be127d13688
    vnd.docker.reference.type:   attestation-manifest
```

## SBOM

> Note: Gatekeeper generates 2 SBOMs. First is for the build stage which includes the builder image and Gatekeeper source code. Second is for the final stage that includes the built Gatekeeper binary (`manager`).

To retrieve [SBOM](https://docs.docker.com/build/attestations/sbom/) for all architectures, please run:

```shell
docker buildx imagetools inspect openpolicyagent/gatekeeper:v3.12.0-rc.0 --format '{{ json .SBOM }}'
```

For specific architecutes (like `linux/amd64`), please run:
```shell
docker buildx imagetools inspect openpolicyagent/gatekeeper:v3.12.0-rc.0 --format '{{ json .SBOM }}' | jq -r '.["linux/amd64"]'
```

## SLSA Provenance

To retrieve [SLSA provenance](https://docs.docker.com/build/attestations/slsa-provenance/), please run:

```shell
docker buildx imagetools inspect openpolicyagent/gatekeeper:v3.12.0-rc.0 --format '{{ json .Provenance }}'
```
