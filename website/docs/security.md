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

# Vulnerability-Patched Images

In addition to the canonical release images, Gatekeeper publishes automatically **patched** variants of recent releases that pick up fixed versions of Go dependencies and the Go standard library **without waiting for the next release**. They are produced on a weekly schedule using [Project Copacetic (Copa)](https://github.com/project-copacetic/copacetic) and published to the same repositories as the regular `gatekeeper` and `gator` images:

- Docker Hub: `openpolicyagent/gatekeeper`, `openpolicyagent/gator`
- GHCR: `ghcr.io/open-policy-agent/gatekeeper`, `ghcr.io/open-policy-agent/gator`

The latest stable release and the previous minor release are patched. The `gatekeeper-crds` image is not patched (it contains no Gatekeeper binary).

## Tags

For a release `vX.Y.Z`, patched images are published under **new, separate** tags — the canonical `vX.Y.Z` tag is never modified:

| Tag | Meaning |
| --- | --- |
| `vX.Y.Z` | The original, canonical release. **Never** changed by patching. |
| `vX.Y.Z-R` | An **immutable** patch revision (`R` = `1`, `2`, …). Each run that fixes newly-available CVEs publishes the next number. |
| `vX.Y.Z-patched` | A **floating** tag that always points at the newest `vX.Y.Z-R`. Use this to track the latest patched build of that release. |

All published architectures `linux/amd64`, `linux/arm64`, and `linux/arm/v7` are rebuilt and patched.

## How patched images are produced

1. The published release image is scanned with [Trivy](https://trivy.dev/) for **fixable** Go-library vulnerabilities.
2. If any are found, Copa rebuilds the affected Go binaries from the corresponding public source, pulling in the fixed dependency and standard-library versions.
3. The result is **re-scanned** to confirm the vulnerability count went down; a patch that does not reduce vulnerabilities is discarded and never published.
4. The patched image is **re-wrapped** so it carries the same [SBOM and SLSA provenance attestations](#build-attestations) as canonical images.

Because the binaries are recompiled, patched images differ from the originals (build timestamps, toolchain version, and so on), so their digests will not match the canonical `vX.Y.Z` image. This is expected.

### Best-effort library patching

Some fixable library CVEs may remain in a patched image. Copa upgrades vulnerable Go modules and runs `go mod tidy`, but a fix can be out of reach when:

- the fixed version requires a **newer Go toolchain** than the rebuild uses;
- a **transitive dependency** holds the module below its fixed version; or
- module resolution is **not deterministic** — the same source can land different versions across architectures or across runs, so a package may be fixed on one architecture but not another, and re-running does not reliably help.

Each run's job summary lists any packages that could not be raised to their fixed version. Patched images therefore **reduce** vulnerabilities on a best-effort basis and are not guaranteed to be free of all fixable CVEs; upgrading to a release that fixes the issue at its source remains the complete fix.

## Trust tier

Patched images are a **complementary, best-effort security refresh** — not a replacement for canonical releases:

- They carry the **same attestation format** (BuildKit SBOM + SLSA provenance) and are verified exactly like canonical images (see [Build Attestations](#build-attestations)). The SBOM reflects the patched contents; the provenance describes the automated patch pipeline rather than the full release pipeline.
- They are rebuilt from **public** upstream source and are **not** run through the full release end-to-end test suite.
- Use `vX.Y.Z-patched` (or a pinned `vX.Y.Z-R`) when you want dependency and standard-library CVE fixes ahead of the next tagged release. Use the canonical `vX.Y.Z` tag when you require the fully-tested, canonical artifact.

## Verifying a patched image

Patched images are inspected and verified exactly like canonical images:

```shell
# List platforms and attestation manifests
docker buildx imagetools inspect openpolicyagent/gatekeeper:vX.Y.Z-patched

# SBOM (all platforms)
docker buildx imagetools inspect openpolicyagent/gatekeeper:vX.Y.Z-patched --format '{{ json .SBOM }}'

# SLSA provenance
docker buildx imagetools inspect openpolicyagent/gatekeeper:vX.Y.Z-patched --format '{{ json .Provenance }}'
```
