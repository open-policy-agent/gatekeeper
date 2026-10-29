#!/usr/bin/env bats

load helpers

request_audit() {
  local nonce
  nonce="$(cat /proc/sys/kernel/random/uuid)"
  kubectl apply -f - -o jsonpath='{.metadata.generation}' <<EOF
apiVersion: audit.gatekeeper.sh/v1alpha1
kind: AuditTrigger
metadata:
  name: audit-trigger
spec:
  auditNonce: "${nonce}"
EOF
}

generation_succeeded() {
  local generation="$1"
  local successful
  successful="$(kubectl get audittrigger audit-trigger -o jsonpath='{.status.successfulGeneration}')"
  [[ "${successful:-0}" -ge "${generation}" ]]
}

@test "AuditTrigger rejects other singleton names and empty nonces" {
  run kubectl create -f - <<EOF
apiVersion: audit.gatekeeper.sh/v1alpha1
kind: AuditTrigger
metadata:
  name: other
spec:
  auditNonce: valid-nonce
EOF
  [ "$status" -ne 0 ]
  [[ "$output" == *"must be named audit-trigger"* ]]

  run kubectl apply -f - <<EOF
apiVersion: audit.gatekeeper.sh/v1alpha1
kind: AuditTrigger
metadata:
  name: audit-trigger
spec:
  auditNonce: ""
EOF
  [ "$status" -ne 0 ]
  [[ "$output" == *"auditNonce"* ]]
}

@test "AuditTrigger nonce requests an audit with periodic audits disabled" {
  local generation
  generation="$(request_audit)"
  wait_for_process 180 1 "generation_succeeded ${generation}"
  local phase
  phase="$(kubectl get audittrigger audit-trigger -o jsonpath='{.status.lastRun.phase}')"
  [ "${phase}" = "Succeeded" ]
}

@test "AuditTrigger changes advance generation and preserve completion" {
  local first latest
  first="$(request_audit)"
  wait_for_process 180 1 "generation_succeeded ${first}"
  for _ in {1..5}; do
    latest="$(request_audit)"
  done
  [ "${latest}" -gt "${first}" ]
  generation_succeeded "${first}"
  wait_for_process 180 1 "generation_succeeded ${latest}"
  local nonce unchanged
  nonce="$(kubectl get audittrigger audit-trigger -o jsonpath='{.spec.auditNonce}')"
  unchanged="$(kubectl patch audittrigger audit-trigger --type=merge \
    -p "{\"spec\":{\"auditNonce\":\"${nonce}\"}}" -o jsonpath='{.metadata.generation}')"
  [ "${unchanged}" = "${latest}" ]
}

@test "AuditTrigger completion survives audit pod restart" {
  local generation
  generation="$(request_audit)"
  wait_for_process 180 1 "generation_succeeded ${generation}"
  kubectl -n "${GATEKEEPER_NAMESPACE:-gatekeeper-system}" rollout restart deployment/gatekeeper-audit
  kubectl -n "${GATEKEEPER_NAMESPACE:-gatekeeper-system}" rollout status deployment/gatekeeper-audit --timeout=180s
  generation_succeeded "${generation}"
}
