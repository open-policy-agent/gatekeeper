---
id: install
title: Installation
---

## Prerequisites

### Minimum Kubernetes Version

The minimum supported Kubernetes version for Gatekeeper is aligned with the Kubernetes releases listed in the [Kubernetes Supported Versions policy](https://kubernetes.io/releases/version-skew-policy/). For more information, please see [supported Kubernetes versions](https://github.com/open-policy-agent/gatekeeper/blob/master/docs/Release_Management.md#supported-kubernetes-versions).

**Note:** Gatekeeper requires resources introduced in Kubernetes v1.16.

### RBAC Permissions

For either installation method, make sure you have cluster admin permissions:

```sh
  kubectl create clusterrolebinding cluster-admin-binding \
    --clusterrole cluster-admin \
    --user <YOUR USER NAME>
```

## Installation

### Deploying a Release using Prebuilt Image

If you want to deploy a released version of Gatekeeper in your cluster with a prebuilt image, then you can run the following command:

```sh
kubectl apply -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/deploy/gatekeeper.yaml
```

### Deploying a Release using development image

If you want to deploy latest development version of Gatekeeper, you can use `openpolicyagent/gatekeeper:dev` tag or `openpolicyagent/gatekeeper:<SHA>`.

Images are hosted in [OPA Docker Hub repository](https://hub.docker.com/r/openpolicyagent/gatekeeper/tags).

### Deploying HEAD Using make

Currently the most reliable way of installing Gatekeeper is to build and install from HEAD:

   * Make sure that:
       * You have [Docker](https://docs.docker.com/engine/install/) version 20.10 or later installed.
       * Your kubectl context is set to the desired installation cluster.
       * You have a container registry you can write to that is readable by the target cluster.

   * Clone the Gatekeeper repository to your local system:
     ```sh
     git clone https://github.com/open-policy-agent/gatekeeper.git
     ```

   * `cd` to the repository directory.

   * Build and push Gatekeeper image:
      ```sh
      export DESTINATION_GATEKEEPER_IMAGE=<add registry like "myregistry.docker.io/gatekeeper">
      make docker-buildx REPOSITORY=$DESTINATION_GATEKEEPER_IMAGE OUTPUT_TYPE=type=registry
      ```

      > If you want to use a local image, don't set OUTPUT_TYPE and it will default to `OUTPUT_TYPE=type=docker`.

   * Finally, deploy:
     ```sh
     make deploy REPOSITORY=$DESTINATION_GATEKEEPER_IMAGE
     ```

### Deploying via Helm

A basic Helm chart exists in `charts/gatekeeper`. If you have Helm installed, you can deploy via the following instructions for Helm v3:

```sh
helm repo add gatekeeper https://open-policy-agent.github.io/gatekeeper/charts
helm install gatekeeper/gatekeeper --name-template=gatekeeper --namespace gatekeeper-system --create-namespace
```

If you are using the older Gatekeeper Helm repo location and Helm v3.3.2+, then use `force-update` to override the default behavior to update the existing repo.

```sh
helm repo add gatekeeper https://open-policy-agent.github.io/gatekeeper/charts --force-update
```

Please note that this chart is compatible with Helm v3 starting with Gatekeeper v3.1.1. When using Helm v3, it is expected to see warnings regarding to `crd-install` hook. This is due to maintaining backwards compatibility with Helm v2 and should not impact the chart deployment.

You can alter the variables in `charts/gatekeeper/values.yaml` to customize your deployment. To regenerate the base template, run `make manifests`.

### Configuring Pod Annotations for Cluster Autoscaler

If you are using the [Kubernetes Cluster Autoscaler](https://github.com/kubernetes/autoscaler/tree/master/cluster-autoscaler), you may want to allow downscaling of nodes running Gatekeeper pods. Since the Gatekeeper audit pod uses an `emptyDir` volume named `tmp-volume` for `/tmp/audit`, the Cluster Autoscaler will block node downscaling by default.

To enable safe eviction for the audit pod, you can pass the `safe-to-evict` pod annotation via Helm:

```sh
helm upgrade --install gatekeeper gatekeeper/gatekeeper \
    --namespace gatekeeper-system \
    --create-namespace \
    --set-string \
    'auditPodAnnotations.cluster-autoscaler\.kubernetes\.io/safe-to-evict-local-volumes=tmp-volume'
```

:::warning
When violation export is enabled with exportBackend=disk, the audit pod has
another local volume, named `tmp-violations` by default. It contains audit
results being handed from Gatekeeper to the export sidecar. Do not include
this volume in safe-to-evict-local-volumes unless losing incomplete or
unconsumed export files during pod eviction is acceptable.

To explicitly allow eviction despite both local volumes:

```sh
helm upgrade --install gatekeeper gatekeeper/gatekeeper \
    --namespace gatekeeper-system \
    --create-namespace \
    --set enableViolationExport=true \
    --set exportBackend=disk \
    --set-string \
    'auditPodAnnotations.cluster-autoscaler\.kubernetes\.io/safe-to-evict-local-volumes=tmp-volume\,tmp-violations'
```

If `audit.exportVolume.name` is customized, use that volume name instead of
`tmp-violations`.
:::

## Uninstallation

### Using Prebuilt Image

If you used a prebuilt image to deploy Gatekeeper, then you can delete all the Gatekeeper components with the following command:

  ```sh
  kubectl delete -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/master/deploy/gatekeeper.yaml
  ```

### Using make

If you used `make` to deploy, then run the following to uninstall Gatekeeper:

   * cd to the repository directory
   * run `make uninstall`

### Using Helm

If you used `helm` to deploy, then run the following to uninstall Gatekeeper:
```sh
helm delete gatekeeper --namespace gatekeeper-system
```

Helm v3 will not cleanup Gatekeeper installed CRDs. Run the following to uninstall Gatekeeper CRDs:
```sh
kubectl delete crd -l gatekeeper.sh/system=yes
```

This operation will also delete any user installed config changes, and constraint templates and constraints.
