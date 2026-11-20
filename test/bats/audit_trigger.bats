#!/usr/bin/env bats

# Run alone on a dedicated cluster with enableAuditTrigger=true and one audit replica:
# make test-e2e BATS_TESTS_FILE=test/bats/audit_trigger.bats
# The suite temporarily changes audit arguments/replicas and the singleton nonce.

load helpers
set -o pipefail

BATS_TESTS_DIR=${BATS_TESTS_DIR:-test/bats/tests}
GATEKEEPER_NAMESPACE=${GATEKEEPER_NAMESPACE:-gatekeeper-system}
AUDIT_TEST_NAMESPACE=gatekeeper-audit-trigger-test
AUDIT_TEST_KIND=k8saudittriggerlabels
AUDIT_TEST_CONSTRAINT=audit-trigger-labels
WAIT_TIME=120

audit_trigger_wait() {
  local deadline=$((SECONDS + WAIT_TIME))
  until "$@"; do
    if (( SECONDS >= deadline )); then
      echo "timed out waiting for $*" >&2
      return 1
    fi
    sleep 1
  done
}

set_audit_trigger_mode() {
  local enabled="$1" interval="$2" cache_dir="${3:-}" replicas
  jq --argjson enabled "${enabled}" --arg interval "${interval}" --arg cache_dir "${cache_dir}" '
    .spec.template.spec.containers[0].args |=
      (map(select((startswith("--enable-audit-trigger=") or startswith("--audit-interval=")) | not))
       + ["--audit-interval=" + $interval]
       + (if $enabled then ["--enable-audit-trigger=true"] else [] end)
       | if $cache_dir != "" then
           map(select((startswith("--api-cache-dir=") or startswith("--audit-chunk-size=")) | not))
           + ["--audit-chunk-size=1", "--api-cache-dir=" + $cache_dir]
         else . end)
  ' "${BATS_FILE_TMPDIR}/original-args.json" > "${BATS_FILE_TMPDIR}/mode.json" || return 1
  kubectl -n "${GATEKEEPER_NAMESPACE}" patch deployment gatekeeper-audit --type=strategic \
    --patch-file "${BATS_FILE_TMPDIR}/mode.json" || return 1
  kubectl -n "${GATEKEEPER_NAMESPACE}" rollout status deployment/gatekeeper-audit --timeout=180s || return 1
  replicas="$(kubectl -n "${GATEKEEPER_NAMESPACE}" get deployment gatekeeper-audit -o jsonpath='{.spec.replicas}')" || return 1
  # A rolled-out Deployment can still have terminating audit processes.
  if [[ "${replicas}" == 0 ]]; then
    audit_trigger_wait audit_trigger_pods_gone
  else
    audit_trigger_wait audit_trigger_one_pod_ready
  fi
}

audit_trigger_policy_ready() {
  local pod constraint
  pod="$(kubectl -n "${GATEKEEPER_NAMESPACE}" get pods -l control-plane=audit-controller -o json |
    jq -er '[.items[] | select(.metadata.deletionTimestamp == null)] | select(length == 1) | .[0].metadata.name')" || return 1
  constraint="$(kubectl get "${AUDIT_TEST_KIND}" "${AUDIT_TEST_CONSTRAINT}" -o json)" || return 1
  jq -e --arg pod "${pod}" '
    .metadata.generation as $generation |
    any(.status.byPod[]?;
      .id == $pod and .observedGeneration == $generation and
      ((.operations // []) | index("audit")) != null and ((.errors // []) | length) == 0)
  ' <<<"${constraint}" >/dev/null
}

audit_trigger_pods_gone() {
  kubectl -n "${GATEKEEPER_NAMESPACE}" get pods -l control-plane=audit-controller -o json |
    jq -e '.items | length == 0' >/dev/null
}

audit_trigger_one_pod_ready() {
  kubectl -n "${GATEKEEPER_NAMESPACE}" get pods -l control-plane=audit-controller -o json |
    jq -e '.items | length == 1 and .[0].metadata.deletionTimestamp == null and
      any(.[0].status.containerStatuses[]?; .name == "manager" and .ready)' >/dev/null
}

request_audit() {
  local nonce="${1:-bats-audit-${BATS_TEST_NUMBER}-${RANDOM}-${RANDOM}}" current
  current="$(kubectl get audittrigger audit-trigger --ignore-not-found -o name)" || return 1
  if [[ -z "${current}" ]]; then
    jq -n --arg nonce "${nonce}" '{
      apiVersion:"audit.gatekeeper.sh/v1alpha1",kind:"AuditTrigger",
      metadata:{name:"audit-trigger"},spec:{auditNonce:$nonce}
    }' | kubectl create -f - -o json > "${BATS_TEST_TMPDIR}/request.json" || return 1
    touch "${BATS_FILE_TMPDIR}/created-trigger"
  else
    jq -n --arg nonce "${nonce}" '{spec:{auditNonce:$nonce}}' > "${BATS_TEST_TMPDIR}/nonce.json"
    kubectl patch audittrigger audit-trigger --type=merge \
      --patch-file "${BATS_TEST_TMPDIR}/nonce.json" -o json > "${BATS_TEST_TMPDIR}/request.json" || return 1
  fi
  AUDIT_REQUEST_UID="$(jq -er '.metadata.uid' "${BATS_TEST_TMPDIR}/request.json")"
  AUDIT_REQUEST_GENERATION="$(jq -er '.metadata.generation' "${BATS_TEST_TMPDIR}/request.json")"
}

audit_request_succeeded() {
  kubectl get audittrigger audit-trigger -o json |
    jq -e --arg uid "${AUDIT_REQUEST_UID}" --argjson generation "${AUDIT_REQUEST_GENERATION}" '
      .metadata.uid == $uid and (.status.successfulGeneration // 0) >= $generation
    ' >/dev/null
}

audit_request_failed() {
  kubectl get audittrigger audit-trigger -o json |
    jq -e --arg uid "${AUDIT_REQUEST_UID}" --argjson generation "${AUDIT_REQUEST_GENERATION}" '
      .metadata.uid == $uid and (.status.successfulGeneration // 0) < $generation and
      (.status.failedGeneration // 0) >= $generation
    ' >/dev/null
}

audit_request_pending() {
  kubectl get audittrigger audit-trigger -o json |
    jq -e --arg uid "${AUDIT_REQUEST_UID}" --argjson generation "${AUDIT_REQUEST_GENERATION}" '
      .metadata.uid == $uid and .metadata.generation == $generation and
      (.status.acknowledgedGeneration // 0) < $generation and
      (.status.successfulGeneration // 0) < $generation and
      (.status.failedGeneration // 0) < $generation
    ' >/dev/null
}

audit_published_result() {
  kubectl get "${AUDIT_TEST_KIND}" "${AUDIT_TEST_CONSTRAINT}" -o json |
    jq -cS '.status | {auditTimestamp,totalViolations,violations:(.violations // [])}'
}

audit_result_matches() {
  local total="$1" previous_timestamp="$2"
  audit_published_result | jq -e --arg namespace "${AUDIT_TEST_NAMESPACE}" \
    --argjson total "${total}" --arg previous "${previous_timestamp}" '
      (.auditTimestamp // "") != "" and .auditTimestamp != $previous and .totalViolations == $total and
      (if $total == 0 then (.violations | length) == 0 else
        (.violations | length) == 1 and
        .violations[0].namespace == $namespace and .violations[0].name == "audit-trigger-bad" and
        .violations[0].kind == "ConfigMap" and .violations[0].enforcementAction == "dryrun"
      end)
    ' >/dev/null
}

setup_file() {
  local deployment existing
  deployment="$(kubectl -n "${GATEKEEPER_NAMESPACE}" get deployment gatekeeper-audit -o json)"
  jq -e '.spec.replicas == 1 and any(.spec.template.spec.containers[];
    .name == "manager" and .securityContext.readOnlyRootFilesystem == true and
    any(.args[]; . == "--enable-audit-trigger=true"))' <<<"${deployment}" >/dev/null
  kubectl wait --for=condition=Established --timeout=60s crd/audittriggers.audit.gatekeeper.sh
  existing="$(kubectl get namespace "${AUDIT_TEST_NAMESPACE}" --ignore-not-found -o name)"
  assert_equal "" "${existing}"
  existing="$(kubectl get constrainttemplate "${AUDIT_TEST_KIND}" --ignore-not-found -o name)"
  assert_equal "" "${existing}"
  kubectl get audittrigger audit-trigger --ignore-not-found -o json > "${BATS_FILE_TMPDIR}/original-trigger.json"
  jq '{spec:{template:{spec:{containers:[.spec.template.spec.containers[] |
    select(.name == "manager") | {name,args}]}}}}' <<<"${deployment}" > "${BATS_FILE_TMPDIR}/original-args.json"
  jq -r '.spec.replicas' <<<"${deployment}" > "${BATS_FILE_TMPDIR}/original-replicas"
  set_audit_trigger_mode true 0

  kubectl create namespace "${AUDIT_TEST_NAMESPACE}"
  touch "${BATS_FILE_TMPDIR}/created-namespace"
  kubectl -n "${AUDIT_TEST_NAMESPACE}" create configmap audit-trigger-good
  kubectl -n "${AUDIT_TEST_NAMESPACE}" label configmap audit-trigger-good gatekeeper=yes
  kubectl -n "${AUDIT_TEST_NAMESPACE}" create configmap audit-trigger-bad
  kubectl -n "${AUDIT_TEST_NAMESPACE}" label configmap audit-trigger-good audit-trigger-bad gatekeeper.sh/audit-trigger-test=yes

  kubectl create --dry-run=client -f "${BATS_TESTS_DIR}/templates/k8srequiredlabels_template.yaml" -o json |
    jq '.metadata.name = "k8saudittriggerlabels" | .spec.crd.spec.names.kind = "K8sAuditTriggerLabels"' |
    kubectl create -f -
  touch "${BATS_FILE_TMPDIR}/created-template"
  audit_trigger_wait kubectl get crd/k8saudittriggerlabels.constraints.gatekeeper.sh
  kubectl wait --for=condition=Established --timeout=60s crd/k8saudittriggerlabels.constraints.gatekeeper.sh
  jq -n --arg namespace "${AUDIT_TEST_NAMESPACE}" --arg name "${AUDIT_TEST_CONSTRAINT}" '{
    apiVersion:"constraints.gatekeeper.sh/v1beta1",kind:"K8sAuditTriggerLabels",
    metadata:{name:$name,labels:{"gatekeeper.sh/tests":"yes"}},
    spec:{enforcementAction:"dryrun",match:{
      kinds:[{apiGroups:[""],kinds:["ConfigMap"]}],namespaces:[$namespace],
      labelSelector:{matchLabels:{"gatekeeper.sh/audit-trigger-test":"yes"}}},
      parameters:{labels:["gatekeeper"]}}
  }' | kubectl create -f -
  touch "${BATS_FILE_TMPDIR}/created-constraint"
  audit_trigger_wait constraint_enforced "${AUDIT_TEST_KIND}" "${AUDIT_TEST_CONSTRAINT}"
  audit_trigger_wait audit_trigger_policy_ready
}

setup() {
  set_audit_trigger_mode true 0
  audit_trigger_wait audit_trigger_policy_ready
  kubectl -n "${AUDIT_TEST_NAMESPACE}" patch configmap audit-trigger-bad --type=merge \
    -p '{"metadata":{"labels":{"gatekeeper":null}}}'
}

teardown() {
  local result=0
  if [[ "${BATS_TEST_COMPLETED}" != 1 ]]; then
    kubectl get audittrigger audit-trigger -o yaml || echo "unable to collect trigger diagnostics" >&2
    kubectl get "${AUDIT_TEST_KIND}" "${AUDIT_TEST_CONSTRAINT}" -o yaml || echo "unable to collect constraint diagnostics" >&2
  fi
  if [[ -f "${BATS_FILE_TMPDIR}/original-args.json" ]]; then
    kubectl -n "${GATEKEEPER_NAMESPACE}" scale deployment gatekeeper-audit --replicas=1 || result=1
    set_audit_trigger_mode true 0 || result=1
  fi
  return "${result}"
}

teardown_file() {
  local result=0
  if [[ -f "${BATS_FILE_TMPDIR}/created-constraint" ]]; then
    kubectl delete "${AUDIT_TEST_KIND}" "${AUDIT_TEST_CONSTRAINT}" --ignore-not-found --timeout=120s || result=1
  fi
  if [[ -f "${BATS_FILE_TMPDIR}/created-template" ]]; then
    kubectl delete constrainttemplate "${AUDIT_TEST_KIND}" --ignore-not-found --timeout=120s || result=1
    kubectl wait --for=delete --timeout=120s crd/k8saudittriggerlabels.constraints.gatekeeper.sh || result=1
  fi
  if [[ -f "${BATS_FILE_TMPDIR}/created-namespace" ]]; then
    kubectl delete namespace "${AUDIT_TEST_NAMESPACE}" --ignore-not-found --timeout=120s || result=1
  fi
  if [[ -f "${BATS_FILE_TMPDIR}/original-args.json" ]]; then
    kubectl -n "${GATEKEEPER_NAMESPACE}" patch deployment gatekeeper-audit --type=strategic \
      --patch-file "${BATS_FILE_TMPDIR}/original-args.json" || result=1
    kubectl -n "${GATEKEEPER_NAMESPACE}" scale deployment gatekeeper-audit \
      --replicas="$(cat "${BATS_FILE_TMPDIR}/original-replicas")" || result=1
    kubectl -n "${GATEKEEPER_NAMESPACE}" rollout status deployment/gatekeeper-audit --timeout=180s || result=1
    audit_trigger_wait audit_trigger_one_pod_ready || result=1
  fi
  if [[ -s "${BATS_FILE_TMPDIR}/original-trigger.json" ]]; then
    jq '{spec:{auditNonce:.spec.auditNonce}}' "${BATS_FILE_TMPDIR}/original-trigger.json" > "${BATS_FILE_TMPDIR}/restore-nonce.json"
    kubectl patch audittrigger audit-trigger --type=merge \
      --patch-file "${BATS_FILE_TMPDIR}/restore-nonce.json" || result=1
  elif [[ -f "${BATS_FILE_TMPDIR}/created-trigger" ]]; then
    kubectl delete audittrigger audit-trigger --ignore-not-found || result=1
  fi
  return "${result}"
}

@test "on-demand audits publish real results and unchanged nonces do not rerun" {
  local previous nonce generation result
  previous="$(audit_published_result | jq -r '.auditTimestamp // ""')"
  request_audit
  audit_trigger_wait audit_request_succeeded
  audit_trigger_wait audit_result_matches 1 "${previous}"
  result="$(audit_published_result)"
  nonce="$(jq -r '.spec.auditNonce' "${BATS_TEST_TMPDIR}/request.json")"
  generation="${AUDIT_REQUEST_GENERATION}"

  request_audit "${nonce}"
  assert_equal "${generation}" "${AUDIT_REQUEST_GENERATION}"
  sleep 3
  assert_equal "${result}" "$(audit_published_result)"

  kubectl -n "${AUDIT_TEST_NAMESPACE}" label configmap audit-trigger-bad gatekeeper=yes --overwrite
  request_audit
  audit_trigger_wait audit_request_succeeded
  audit_trigger_wait audit_result_matches 0 "$(jq -r '.auditTimestamp' <<<"${result}")"
}

@test "incomplete audits fail without clearing results and recover with a fresh nonce" {
  local previous result
  previous="$(audit_published_result | jq -r '.auditTimestamp // ""')"
  request_audit
  audit_trigger_wait audit_request_succeeded
  audit_trigger_wait audit_result_matches 1 "${previous}"
  result="$(audit_published_result)"
  kubectl -n "${AUDIT_TEST_NAMESPACE}" label configmap audit-trigger-bad gatekeeper=yes --overwrite

  set_audit_trigger_mode true 0 /audit-trigger-e2e-missing/cache
  audit_trigger_wait audit_trigger_policy_ready
  request_audit
  audit_trigger_wait audit_request_failed
  assert_equal "${result}" "$(audit_published_result)"

  set_audit_trigger_mode true 0
  audit_trigger_wait audit_trigger_policy_ready
  request_audit
  audit_trigger_wait audit_request_succeeded
  audit_trigger_wait audit_result_matches 0 "$(jq -r '.auditTimestamp' <<<"${result}")"
}

@test "disabled requests remain pending and survive an audit process restart" {
  local previous result
  set_audit_trigger_mode false 2
  previous="$(audit_published_result | jq -r '.auditTimestamp // ""')"
  request_audit
  audit_trigger_wait audit_result_matches 1 "${previous}"
  audit_request_pending

  set_audit_trigger_mode false 0
  result="$(audit_published_result)"
  sleep 3
  audit_request_pending
  assert_equal "${result}" "$(audit_published_result)"

  kubectl -n "${GATEKEEPER_NAMESPACE}" scale deployment gatekeeper-audit --replicas=0
  audit_trigger_wait audit_trigger_pods_gone
  set_audit_trigger_mode true 0
  kubectl -n "${GATEKEEPER_NAMESPACE}" scale deployment gatekeeper-audit --replicas=1
  kubectl -n "${GATEKEEPER_NAMESPACE}" rollout status deployment/gatekeeper-audit --timeout=180s
  audit_trigger_wait audit_request_succeeded
  assert_equal "${AUDIT_REQUEST_GENERATION}" "$(kubectl get audittrigger audit-trigger -o jsonpath='{.metadata.generation}')"
  audit_trigger_wait audit_result_matches 1 "$(jq -r '.auditTimestamp' <<<"${result}")"
}
