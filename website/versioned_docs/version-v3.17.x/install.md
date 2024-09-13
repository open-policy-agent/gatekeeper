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
kubectl apply -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/v3.17.0/deploy/gatekeeper.yaml
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

## Uninstallation

### Using Prebuilt Image

If you used a prebuilt image to deploy Gatekeeper, then you can delete all the Gatekeeper components with the following command:

  ```sh
  kubectl delete -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/v3.17.0/deploy/gatekeeper.yaml
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
