#!/usr/bin/env bash

# Request an audit as soon as possible.
# Usage: ./hack/trigger-audit.sh [kubectl-context]

set -euo pipefail

KUBECTL_ARGS=()
if [[ "$#" -gt 1 ]]; then
  echo "usage: $0 [kubectl-context]" >&2
  exit 1
fi
if [[ "$#" -eq 1 ]]; then
  KUBECTL_ARGS=(--context "$1")
fi

NONCE="$(cat /proc/sys/kernel/random/uuid)"

cat <<EOF | kubectl "${KUBECTL_ARGS[@]}" apply -f - \
  -o jsonpath='uid={.metadata.uid}{"\n"}generation={.metadata.generation}{"\n"}'
apiVersion: audit.gatekeeper.sh/v1alpha1
kind: AuditTrigger
metadata:
  name: audit-trigger
spec:
  auditNonce: "${NONCE}"
EOF
