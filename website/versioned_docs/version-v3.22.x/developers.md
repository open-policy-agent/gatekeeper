---
id: developers
title: Developers
---

This section describes how Gatekeeper developers can leverage [kind](https://kind.sigs.k8s.io/) and [Tilt](https://tilt.dev/) for rapid iterative development. Kind allows developers to quickly provision a conformant Kubernetes cluster using Docker and Tilt enables smart rebuilds and live updates of your Kubernetes workload during development time.

## Prerequisites

1. [kind](https://kind.sigs.k8s.io/#installation-and-usage) v0.11.0 or newer
2. [Tilt](https://docs.tilt.dev/install.html) v0.25.0 or newer

## Getting started

### Create a kind cluster with a local registry

Kind cluster with a local registry will enable faster image pushing and pulling:

```bash
./third_party/github.com/tilt-dev/kind-local/kind-with-registry.sh
```

> If you would like to customize the local registry port on your machine (the default port is `5000`), you can run `export KIND_REGISTRY_PORT=<port>` to customize it.

### Create `tilt-settings.json`

`tilt-settings.json` contains various settings that developers can customize when deploying gatekeeper to a local kind cluster. Developers can create the JSON file under the project root directory:

```json
{
    "helm_values": {
        "controllerManager.metricsPort": 8080,
        "enableExternalData": true
    },
    "trigger_mode": "manual"
}
```

#### `tilt-settings.json` fields

- `helm_values` (Map, default=`{}`): A map of helm values to be injected when deploying `manifest_staging/charts/gatekeeper` to the kind cluster.

- `trigger_mode` (String, default=`"auto"`): Optional setting to configure if tilt should automatically rebuild on changes. Set to `manual` to disable auto-rebuilding and require users to trigger rebuilds of individual changed components through the UI.

### Run `make tilt`

```bash
make tilt
```

<details>
<summary>Output</summary>

```
make tilt
docker build . \
        -t gatekeeper-tooling \
        -f build/tooling/Dockerfile
[+] Building 1.5s (10/10) FINISHED
 => [internal] load build definition from Dockerfile                                                                                                                     0.2s
 => => transferring dockerfile: 35B                                                                                                                                      0.1s
 => [internal] load .dockerignore                                                                                                                                        0.2s
 => => transferring context: 34B                                                                                                                                         0.0s
 => [internal] load metadata for docker.io/library/golang:1.17                                                                                                           1.0s
 => [auth] library/golang:pull token for registry-1.docker.io                                                                                                            0.0s
 => [1/5] FROM docker.io/library/golang:1.17@sha256:bd9823cdad5700fb4abe983854488749421d5b4fc84154c30dae474100468b85                                                     0.0s
 => CACHED [2/5] RUN GO111MODULE=on go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0                                                                    0.0s
 => CACHED [3/5] RUN GO111MODULE=on go install k8s.io/code-generator/cmd/conversion-gen@release-1.22                                                                     0.0s
 => CACHED [4/5] RUN mkdir /gatekeeper                                                                                                                                   0.0s
 => CACHED [5/5] WORKDIR /gatekeeper                                                                                                                                     0.0s
 => exporting to image                                                                                                                                                   0.2s
 => => exporting layers                                                                                                                                                  0.0s
 => => writing image sha256:7d2fecb230986ffdd78932ad8ff13aa0968c9a9a98bec2fe8ecb21c6e683c730                                                                             0.0s
 => => naming to docker.io/library/gatekeeper-tooling                                                                                                                    0.0s
docker run -v /workspaces/gatekeeper:/gatekeeper gatekeeper-tooling controller-gen object:headerFile=./hack/boilerplate.go.txt paths="./apis/..." paths="./pkg/..."
docker run -v /workspaces/gatekeeper:/gatekeeper gatekeeper-tooling conversion-gen \
        --output-base=/gatekeeper \
        --input-dirs=./apis/mutations/v1beta1,./apis/mutations/v1alpha1 \
        --go-header-file=./hack/boilerplate.go.txt \
        --output-file-base=zz_generated.conversion
docker run -v /workspaces/gatekeeper:/gatekeeper gatekeeper-tooling controller-gen \
        crd \
        rbac:roleName=manager-role \
        webhook \
        paths="./apis/..." \
        paths="./pkg/..." \
        output:crd:artifacts:config=config/crd/bases
rm -rf manifest_staging
mkdir -p manifest_staging/deploy/experimental
mkdir -p manifest_staging/charts/gatekeeper
docker run --rm -v /workspaces/gatekeeper:/gatekeeper \
        registry.k8s.io/kustomize/kustomize:v3.8.9 build \
        /gatekeeper/config/default -o /gatekeeper/manifest_staging/deploy/gatekeeper.yaml
docker run --rm -v /workspaces/gatekeeper:/gatekeeper \
        registry.k8s.io/kustomize/kustomize:v3.8.9 build \
        --load_restrictor LoadRestrictionsNone /gatekeeper/cmd/build/helmify | go run cmd/build/helmify/*.go
Writing manifest_staging/charts/gatekeeper/.helmignore
Writing manifest_staging/charts/gatekeeper/Chart.yaml
Writing manifest_staging/charts/gatekeeper/README.md
Making manifest_staging/charts/gatekeeper/templates
Writing manifest_staging/charts/gatekeeper/templates/_helpers.tpl
Writing manifest_staging/charts/gatekeeper/templates/namespace-post-install.yaml
Writing manifest_staging/charts/gatekeeper/templates/upgrade-crds-hook.yaml
Writing manifest_staging/charts/gatekeeper/templates/webhook-configs-pre-delete.yaml
Writing manifest_staging/charts/gatekeeper/values.yaml
Writing manifest_staging/charts/gatekeeper/templates/gatekeeper-webhook-server-cert-secret.yaml
Writing manifest_staging/charts/gatekeeper/templates/gatekeeper-audit-deployment.yaml
Writing manifest_staging/charts/gatekeeper/templates/gatekeeper-controller-manager-deployment.yaml
Writing manifest_staging/charts/gatekeeper/templates/gatekeeper-validating-webhook-configuration-validatingwebhookconfiguration.yaml
Writing manifest_staging/charts/gatekeeper/templates/gatekeeper-controller-manager-poddisruptionbudget.yaml
Writing manifest_staging/charts/gatekeeper/templates/gatekeeper-admin-serviceaccount.yaml
Writing manifest_staging/charts/gatekeeper/templates/gatekeeper-admin-podsecuritypolicy.yaml
Writing manifest_staging/charts/gatekeeper/templates/gatekeeper-webhook-service-service.yaml
Writing manifest_staging/charts/gatekeeper/templates/gatekeeper-manager-role-clusterrole.yaml
Writing manifest_staging/charts/gatekeeper/templates/gatekeeper-manager-rolebinding-rolebinding.yaml
Writing manifest_staging/charts/gatekeeper/templates/gatekeeper-manager-rolebinding-clusterrolebinding.yaml
Writing manifest_staging/charts/gatekeeper/templates/gatekeeper-mutating-webhook-configuration-mutatingwebhookconfiguration.yaml
Writing manifest_staging/charts/gatekeeper/templates/gatekeeper-critical-pods-resourcequota.yaml
Making manifest_staging/charts/gatekeeper/crds
Writing manifest_staging/charts/gatekeeper/crds/assign-customresourcedefinition.yaml
Writing manifest_staging/charts/gatekeeper/crds/assignmetadata-customresourcedefinition.yaml
Writing manifest_staging/charts/gatekeeper/crds/config-customresourcedefinition.yaml
Writing manifest_staging/charts/gatekeeper/crds/constraintpodstatus-customresourcedefinition.yaml
Writing manifest_staging/charts/gatekeeper/crds/constrainttemplatepodstatus-customresourcedefinition.yaml
Writing manifest_staging/charts/gatekeeper/crds/constrainttemplate-customresourcedefinition.yaml
Writing manifest_staging/charts/gatekeeper/crds/modifyset-customresourcedefinition.yaml
Writing manifest_staging/charts/gatekeeper/crds/mutatorpodstatus-customresourcedefinition.yaml
Writing manifest_staging/charts/gatekeeper/crds/provider-customresourcedefinition.yaml
Writing manifest_staging/charts/gatekeeper/templates/gatekeeper-manager-role-role.yaml
mkdir -p .tiltbuild/charts
rm -rf .tiltbuild/charts/gatekeeper
cp -R manifest_staging/charts/gatekeeper .tiltbuild/charts
# disable some configs from the security context so we can perform live update
sed -i "/readOnlyRootFilesystem: true/d" .tiltbuild/charts/gatekeeper/templates/*.yaml
sed -i -e "/run.*: .*/d" .tiltbuild/charts/gatekeeper/templates/*.yaml
tilt up
Tilt started on http://localhost:10350/
v0.25.2, built 2022-02-25

(space) to open the browser
(s) to stream logs (--stream=true)
(t) to open legacy terminal mode (--legacy=true)
(ctrl-c) to exit
```

</details>

### Start developing!

If you have trigger mode set to `auto`, any changes in the source code will trigger a rebuild of the gatekeeper manager binary. The build will subsequently trigger a rebuild of the gatekeeper manager image, load it to your kind cluster, and restart the deployment.

If you have trigger mode set to `manual`, you can trigger a manager build manually in the local Tilt UI portal. By default, it is located at `http://localhost:10350/`

### Tear down the kind cluster

To tear down the kind cluster and its local registry:

```bash
./third_party/github.com/tilt-dev/kind-local/teardown-kind-with-registry.sh
```
