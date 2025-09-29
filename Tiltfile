# -*- mode: Python -*-

# global settings
settings = {
    "allowed_contexts": [
        "kind-gatekeeper",
    ],
}
settings.update(read_json(
    "tilt-settings.json",
    default={},
))

allow_k8s_contexts(settings.get("allowed_contexts", []))

if settings.get("trigger_mode", "auto").lower() == "manual":
    trigger_mode(TRIGGER_MODE_MANUAL)

TILT_DOCKERFILE = """
FROM golang:1.25-trixie as tilt-helper
# Support live reloading with Tilt
RUN wget --output-document /restart.sh --quiet https://raw.githubusercontent.com/tilt-dev/rerun-process-wrapper/60eaa572cdf825c646008e1ea28b635f83cefb38/restart.sh && \
    wget --output-document /start.sh --quiet https://raw.githubusercontent.com/tilt-dev/rerun-process-wrapper/60eaa572cdf825c646008e1ea28b635f83cefb38/start.sh && \
    chmod +x /start.sh && chmod +x /restart.sh

FROM gcr.io/distroless/base:debug as tilt
WORKDIR /
COPY --from=tilt-helper /start.sh .
COPY --from=tilt-helper /restart.sh .
COPY bin/manager .
"""

# build_manager defines the build process for the manager binary and image.
def build_manager():
    cmd = [
        "make tilt-prepare",
        "GO111MODULE=on CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod vendor -a -o .tiltbuild/bin/manager",
    ]
    local_resource(
        "manager",
        cmd=";".join(cmd),
        deps=["pkg", "third_party", "vendor",
              "apis", "go.mod", "go.sum", "main.go"],
        labels=["bin"],
    )
    docker_build(
        ref="openpolicyagent/gatekeeper",
        dockerfile_contents=TILT_DOCKERFILE,
        context=".tiltbuild",
        target="tilt",
        entrypoint=["sh", "/start.sh", "/manager"],
        only="bin/manager",
        live_update=[
            sync(".tiltbuild/bin/manager", "/manager"),
            run("sh /restart.sh"),
        ],
    )

# build_crds defines the build process for the CRDs image.
def build_crds():
    local_resource(
        "crds",
        cmd=";".join(["rm -rf .staging", "mkdir -p .staging/crds",
                     "cp -R .tiltbuild/charts/gatekeeper/crds/ .staging/crds/"]),
        deps=[".tiltbuild/charts/gatekeeper/crds"],
        labels=["staging"],
    )

    docker_build(
        ref="openpolicyagent/gatekeeper-crds",
        dockerfile="./crd.Dockerfile",
        context=".staging/crds/",
        target="build",
        only="crds",
        build_args={"KUBE_VERSION": "1.28.0"},
        live_update=[
            sync(".staging/crds/", "/crds"),
        ],
    )

# deploy_gatekeeper defines the deploy process for the gatekeeper chart from manifest_staging/charts/gatekeeper.
def deploy_gatekeeper():
    local("kubectl create namespace gatekeeper-system || true")
    
    local_resource(
        name="generate-helm-values",
        cmd="cat tilt-settings.json | jq '.helm_values' > .tiltbuild/helm_values.generated.yaml",
        deps=["tilt-settings.json"],
        labels=["helm"],
    )
    
    helm_values = settings.get("helm_values", {})

    k8s_yaml(helm(
        ".tiltbuild/charts/gatekeeper",
        name="gatekeeper",
        namespace="gatekeeper-system",
        values=[".tiltbuild/charts/gatekeeper/values.yaml", ".tiltbuild/helm_values.generated.yaml"],
    ))

    # add label to resources
    for resource in ["gatekeeper-audit", "gatekeeper-controller-manager", "gatekeeper-update-namespace-label", "gatekeeper-update-crds-hook"]:
        k8s_resource(resource, labels=["controllers"])

    # port-foward the metrics server
    if "audit.metricsPort" in helm_values:
        port = int(helm_values["audit.metricsPort"])
        k8s_resource(
            workload="gatekeeper-audit",
            port_forwards=[port_forward(
                port, name="View metrics", link_path="/metrics")],
        )

    if "controllerManager.metricsPort" in helm_values:
        port = int(helm_values["controllerManager.metricsPort"])
        k8s_resource(
            workload="gatekeeper-controller-manager",
            port_forwards=[port_forward(
                port, name="View metrics", link_path="/metrics")],
        )


build_manager()

build_crds()

deploy_gatekeeper()
