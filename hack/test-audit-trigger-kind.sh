#!/usr/bin/env bash

# End-to-end AuditTrigger smoke test using a plain Docker Hub image and Kind.
#
# This intentionally avoids Gatekeeper's full e2e harness. It:
#   1. builds and pushes the current worktree to Docker Hub,
#   2. creates an isolated Kind cluster,
#   3. generates and patches the standard deployment manifest,
#   4. deploys Gatekeeper with --audit-interval=0,
#   5. proves no periodic audit runs, and
#   6. proves a nonce update starts and completes an audit.
#
# Prerequisites:
#   - docker, kind, kubectl, and make
#   - Docker authenticated to Docker Hub with push access to IMAGE_REPOSITORY
#
# Usage:
#   ./hack/test-audit-trigger-kind.sh [audit-interval-seconds]
#
# Examples:
#   ./hack/test-audit-trigger-kind.sh       # on-demand audits only
#   ./hack/test-audit-trigger-kind.sh 60    # periodic audit every 60 seconds
#
# Optional overrides:
#   IMAGE_REPOSITORY=docker.io/anlandu/gatekeeper \
#   IMAGE_TAG=audit-trigger-manual \
#   KIND_CLUSTER=gk-audit-trigger \
#   ./hack/test-audit-trigger-kind.sh
#
# Reuse an image that has already been pushed:
#   SKIP_BUILD=true \
#   IMAGE_TAG=audit-trigger-df42397b8-20261006 \
#   ./hack/test-audit-trigger-kind.sh
#
# The cluster is deliberately preserved after the test for inspection. Delete it
# when finished with:
#   kind delete cluster --name "${KIND_CLUSTER:-gk-audit-trigger}"

set -euo pipefail

IMAGE_REPOSITORY="${IMAGE_REPOSITORY:-docker.io/anlandu/gatekeeper}"
IMAGE_TAG="${IMAGE_TAG:-audit-trigger-$(git rev-parse --short HEAD)-$(date -u +%Y%m%d%H%M%S)}"
IMAGE="${IMAGE_REPOSITORY}:${IMAGE_TAG}"
SKIP_BUILD="${SKIP_BUILD:-false}"
AUDIT_INTERVAL="${1:-0}"
KIND_CLUSTER="${KIND_CLUSTER:-gk-audit-trigger}"
KUBE_CONTEXT="kind-${KIND_CLUSTER}"
GATEKEEPER_NAMESPACE="${GATEKEEPER_NAMESPACE:-gatekeeper-system}"
AUDIT_TRIGGER_NAME="audit-trigger"

MANIFEST="$(mktemp)"
PREVIOUS_CONTEXT="$(kubectl config current-context 2>/dev/null || true)"

cleanup() {
  rm -f "${MANIFEST}"
  if [[ -n "${PREVIOUS_CONTEXT}" ]]; then
    kubectl config use-context "${PREVIOUS_CONTEXT}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

if [[ "$#" -gt 1 || ! "${AUDIT_INTERVAL}" =~ ^[0-9]+$ ]]; then
  echo "usage: $0 [audit-interval-seconds]" >&2
  exit 1
fi

for command in docker kind kubectl make git; do
  if ! command -v "${command}" >/dev/null 2>&1; then
    echo "required command not found: ${command}" >&2
    exit 1
  fi
done

if [[ "${SKIP_BUILD}" == "true" ]]; then
  echo "==> Reusing previously pushed image ${IMAGE}"
  # Fail before creating or modifying a cluster if the requested tag is absent.
  docker buildx imagetools inspect "${IMAGE}" >/dev/null
else
  echo "==> Building and pushing ${IMAGE}"
  # --push makes the test exercise the same image-pull path a normal cluster uses.
  docker buildx build \
    --platform linux/amd64 \
    --push \
    --tag "${IMAGE}" \
    .
fi

if kind get clusters | grep -Fxq "${KIND_CLUSTER}"; then
  echo "==> Reusing Kind cluster ${KIND_CLUSTER}"
else
  echo "==> Creating Kind cluster ${KIND_CLUSTER}"
  kind create cluster --name "${KIND_CLUSTER}" --wait 120s
fi

echo "==> Generating Gatekeeper CRDs, RBAC, and deployment manifests"
make manifests

echo "==> Preparing a manifest that uses ${IMAGE} with audit interval ${AUDIT_INTERVAL}"
# The generated deployment has two Gatekeeper containers: audit and webhook.
# Replace both images, then add the requested interval only to the audit deployment.
sed -E \
  -e "s#image: openpolicyagent/gatekeeper:[^[:space:]]+#image: ${IMAGE}#g" \
  -e "/- --operation=audit/a\\        - --audit-interval=${AUDIT_INTERVAL}" \
  manifest_staging/deploy/gatekeeper.yaml >"${MANIFEST}"

echo "==> Deploying Gatekeeper"
kubectl --context "${KUBE_CONTEXT}" apply -f "${MANIFEST}"

echo "==> Waiting for Gatekeeper deployments"
kubectl --context "${KUBE_CONTEXT}" \
  --namespace "${GATEKEEPER_NAMESPACE}" \
  rollout status deployment/gatekeeper-controller-manager \
  --timeout=180s
kubectl --context "${KUBE_CONTEXT}" \
  --namespace "${GATEKEEPER_NAMESPACE}" \
  rollout status deployment/gatekeeper-audit \
  --timeout=180s

echo "==> Verifying the audit pod uses the pushed image and requested interval"
kubectl --context "${KUBE_CONTEXT}" \
  --namespace "${GATEKEEPER_NAMESPACE}" \
  get deployment gatekeeper-audit \
  -o jsonpath='image={.spec.template.spec.containers[0].image}{"\n"}args={.spec.template.spec.containers[0].args}{"\n"}'

AUDIT_IMAGE="$(
  kubectl --context "${KUBE_CONTEXT}" \
    --namespace "${GATEKEEPER_NAMESPACE}" \
    get deployment gatekeeper-audit \
    -o jsonpath='{.spec.template.spec.containers[0].image}'
)"
if [[ "${AUDIT_IMAGE}" != "${IMAGE}" ]]; then
  echo "audit deployment image ${AUDIT_IMAGE} does not match ${IMAGE}" >&2
  exit 1
fi

AUDIT_INTERVAL_ARG="$(
  kubectl --context "${KUBE_CONTEXT}" \
    --namespace "${GATEKEEPER_NAMESPACE}" \
    get deployment gatekeeper-audit \
    -o go-template='{{range (index .spec.template.spec.containers 0).args}}{{println .}}{{end}}' |
    grep '^--audit-interval='
)"
if [[ "${AUDIT_INTERVAL_ARG}" != "--audit-interval=${AUDIT_INTERVAL}" ]]; then
  echo "audit deployment does not have --audit-interval=${AUDIT_INTERVAL}" >&2
  exit 1
fi

echo "==> Installing a policy that finds existing unlabeled namespaces"
kubectl --context "${KUBE_CONTEXT}" apply \
  -f demo/basic/templates/k8srequiredlabels_template.yaml
kubectl --context "${KUBE_CONTEXT}" wait \
  --for=condition=Established \
  crd/k8srequiredlabels.constraints.gatekeeper.sh \
  --timeout=120s
# Recreate the constraint so a previous run's auditTimestamp cannot satisfy this test.
kubectl --context "${KUBE_CONTEXT}" delete \
  k8srequiredlabels ns-must-have-gk \
  --ignore-not-found
kubectl --context "${KUBE_CONTEXT}" apply \
  -f demo/basic/constraints/all_ns_must_have_gatekeeper.yaml

if [[ "${AUDIT_INTERVAL}" == "0" ]]; then
  echo "==> Confirming no periodic audit occurs while the interval is zero"
  sleep 15
  AUDIT_TIMESTAMP="$(
    kubectl --context "${KUBE_CONTEXT}" \
      get k8srequiredlabels ns-must-have-gk \
      -o jsonpath='{.status.auditTimestamp}'
  )"
  if [[ -n "${AUDIT_TIMESTAMP}" ]]; then
    echo "unexpected periodic audit at ${AUDIT_TIMESTAMP}" >&2
    exit 1
  fi
else
  echo "==> Periodic audit is enabled; skipping the zero-interval assertion"
fi

echo "==> Requesting an immediate audit"
NONCE="$(cat /proc/sys/kernel/random/uuid)"
REQUEST="$(
cat <<EOF | kubectl --context "${KUBE_CONTEXT}" apply -f - \
  -o jsonpath='{.metadata.uid}{" "}{.metadata.generation}'
apiVersion: audit.gatekeeper.sh/v1alpha1
kind: AuditTrigger
metadata:
  name: ${AUDIT_TRIGGER_NAME}
spec:
  auditNonce: "${NONCE}"
EOF
)"
read -r REQUEST_UID REQUEST_GENERATION <<<"${REQUEST}"

echo "==> Waiting for acknowledgement and successful scan completion"
SUCCEEDED=false
for _ in $(seq 1 90); do
  STATUS="$(kubectl --context "${KUBE_CONTEXT}" get "audittrigger/${AUDIT_TRIGGER_NAME}" \
    -o go-template='{{.metadata.uid}} {{if .status.successfulGeneration}}{{.status.successfulGeneration}}{{else}}0{{end}} {{if .status.failedGeneration}}{{.status.failedGeneration}}{{else}}0{{end}}')"
  read -r CURRENT_UID SUCCESS_GENERATION FAILED_GENERATION <<<"${STATUS}"
  if [[ "${CURRENT_UID}" != "${REQUEST_UID}" ]]; then
    echo "AuditTrigger was recreated while waiting" >&2
    exit 1
  fi
  if (( ${SUCCESS_GENERATION:-0} >= REQUEST_GENERATION )); then
    SUCCEEDED=true
    break
  fi
  if (( ${FAILED_GENERATION:-0} >= REQUEST_GENERATION )); then
    echo "Requested audit failed" >&2
    exit 1
  fi
  sleep 2
done
if [[ "${SUCCEEDED}" != "true" ]]; then
  echo "Timed out waiting for audit generation ${REQUEST_GENERATION}" >&2
  exit 1
fi

echo "==> Verifying the requested audit wrote constraint results"
for _ in $(seq 1 60); do
  AUDIT_TIMESTAMP="$(
    kubectl --context "${KUBE_CONTEXT}" \
      get k8srequiredlabels ns-must-have-gk \
      -o jsonpath='{.status.auditTimestamp}'
  )"
  TOTAL_VIOLATIONS="$(
    kubectl --context "${KUBE_CONTEXT}" \
      get k8srequiredlabels ns-must-have-gk \
      -o jsonpath='{.status.totalViolations}'
  )"
  if [[ -n "${AUDIT_TIMESTAMP}" && "${TOTAL_VIOLATIONS:-0}" -gt 0 ]]; then
    break
  fi
  sleep 2
done

if [[ -z "${AUDIT_TIMESTAMP}" || "${TOTAL_VIOLATIONS:-0}" -le 0 ]]; then
  echo "AuditTrigger completed but constraint audit results were not populated" >&2
  kubectl --context "${KUBE_CONTEXT}" \
    --namespace "${GATEKEEPER_NAMESPACE}" \
    logs deployment/gatekeeper-audit \
    --tail=200 >&2
  exit 1
fi

echo "==> AuditTrigger status"
kubectl --context "${KUBE_CONTEXT}" get \
  "audittrigger/${AUDIT_TRIGGER_NAME}" \
  -o yaml

echo "==> Constraint audit summary"
kubectl --context "${KUBE_CONTEXT}" get \
  k8srequiredlabels/ns-must-have-gk \
  -o jsonpath='auditTimestamp={.status.auditTimestamp}{"\n"}totalViolations={.status.totalViolations}{"\n"}'

echo "PASS: AuditTrigger ran an audit with --audit-interval=${AUDIT_INTERVAL}"
echo "Image: ${IMAGE}"
echo "Cluster retained for inspection: ${KIND_CLUSTER}"
echo "Cleanup: kind delete cluster --name ${KIND_CLUSTER}"
