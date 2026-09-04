#!/bin/bash

assert_success() {
  if [[ "$status" != 0 ]]; then
    echo "expected: 0"
    echo "actual: $status"
    echo "output: $output"
    return 1
  fi
}

assert_failure() {
  if [[ "$status" == 0 ]]; then
    echo "expected: non-zero exit code"
    echo "actual: $status"
    echo "output: $output"
    return 1
  fi
}

assert_equal() {
  if [[ "$1" != "$2" ]]; then
    echo "expected: $1"
    echo "actual: $2"
    return 1
  fi
}

assert_not_equal() {
  if [[ "$1" == "$2" ]]; then
    echo "unexpected: $1"
    echo "actual: $2"
    return 1
  fi
}

assert_match() {
  if [[ ! "$2" =~ $1 ]]; then
    echo "expected: $1"
    echo "actual: $2"
    return 1
  fi
}

assert_not_match() {
  if [[ "$2" =~ $1 ]]; then
    echo "expected: $1"
    echo "actual: $2"
    return 1
  fi
}

assert_len() {
  if [[ "$1" != "${#2}" ]]; then
    echo "expected len: $1"
    echo "actual len: ${#2} ($2)"
    return 1
  fi
}

wait_for_process() {
  wait_time="$1"
  sleep_time="$2"
  cmd="$3"
  while [ "$wait_time" -gt 0 ]; do
    if eval "$cmd"; then
      return 0
    else
      sleep "$sleep_time"
      wait_time=$((wait_time - sleep_time))
    fi
  done
  return 1
}

kube_apiserver_audit_log() {
  local control_plane_node
  control_plane_node="$(kubectl get nodes -l node-role.kubernetes.io/control-plane -o jsonpath='{.items[0].metadata.name}')" || return 1
  if [[ -z "${control_plane_node}" ]]; then
    echo "could not find kind control-plane node"
    return 1
  fi

  docker exec "${control_plane_node}" cat /var/log/kubernetes/kube-apiserver-audit.log
}

webhook_admission_audit_annotation_matches() {
  local resource_name="$1"
  local constraint_name="$2"
  local audit_log
  audit_log="$(kube_apiserver_audit_log)" || return 1

  jq -e --arg resource_name "${resource_name}" --arg constraint_name "${constraint_name}" '
    select(.stage == "ResponseComplete" and .objectRef.name == $resource_name)
    | .annotations["validation.gatekeeper.sh/evaluation"]?
    | fromjson?
    | select(
        .schemaVersion == "v1" and
        .eventType == "validation_admission" and
        .allowed == false and
        .totalViolations == 1 and
        .includedViolations == 1 and
        .truncated == false
      )
    | select(any(.violations[]?;
        .constraintKind == "K8sRequiredLabels" and
        .constraintName == $constraint_name and
        .enforcementAction == "deny"
      ))
  ' <<<"${audit_log}" >/dev/null
}

webhook_admission_audit_annotation_without_violations_matches() {
  local resource_name="$1"
  local audit_log
  audit_log="$(kube_apiserver_audit_log)" || return 1

  jq -e --arg resource_name "${resource_name}" '
    select(.stage == "ResponseComplete" and .objectRef.name == $resource_name)
    | .annotations["validation.gatekeeper.sh/evaluation"]?
    | fromjson?
    | select(
        .schemaVersion == "v1" and
        .eventType == "validation_admission" and
        .allowed == true and
        .totalViolations == 0 and
        .includedViolations == 0 and
        .truncated == false and
        (.violations | length) == 0
      )
  ' <<<"${audit_log}" >/dev/null
}

vap_admission_audit_configuration_ready() {
  local policy_name="$1"
  local binding_name="$2"
  local validation_action="${3:-Deny}"
  local value_expression="params == null ? '' : 'true'"
  local policy
  local binding

  policy="$(kubectl get validatingadmissionpolicy "${policy_name}" -o json)" || return 1
  binding="$(kubectl get validatingadmissionpolicybinding "${binding_name}" -o json)" || return 1

  jq -e --arg value_expression "${value_expression}" '
    any(.spec.auditAnnotations[]?;
      .key == "evaluation" and
      .valueExpression == $value_expression
    )
  ' <<<"${policy}" >/dev/null || return 1

  jq -e --arg validation_action "${validation_action}" '
    (.spec.validationActions | index($validation_action)) != null and
    (.spec.validationActions | index("Audit")) != null and
    (.spec.validationActions | length) == (if $validation_action == "Audit" then 1 else 2 end)
  ' <<<"${binding}" >/dev/null
}

vap_admission_enforced() {
  local resource_name="$1"
  local policy_name="$2"
  local binding_name="$3"
  local output

  if output="$(kubectl create namespace "${resource_name}" --dry-run=server 2>&1)"; then
    return 1
  fi

  [[ "${output}" == *"ValidatingAdmissionPolicy '${policy_name}' with binding '${binding_name}' denied request"* ]]
}

vap_admission_audit_annotations_match() {
  local resource_name="$1"
  local policy_name="$2"
  local constraint_name="$3"
  local binding_name="${policy_name}-${constraint_name}"
  local evaluation_key="${policy_name}/evaluation"
  local audit_log
  audit_log="$(kube_apiserver_audit_log)" || return 1

  jq -e \
    --arg resource_name "${resource_name}" \
    --arg evaluation_key "${evaluation_key}" \
    --arg constraint_name "${constraint_name}" \
    --arg policy_name "${policy_name}" \
    --arg binding_name "${binding_name}" '
      select(.stage == "ResponseComplete" and .objectRef.name == $resource_name)
      | select(.annotations[$evaluation_key] == "true")
      | (.annotations["validation.policy.admission.k8s.io/validation_failure"]? | fromjson?) as $failures
      | select(any($failures[]?;
          .policy == $policy_name and
          .binding == $binding_name and
          (.validationActions | index("Deny")) != null and
          (.validationActions | index("Audit")) != null
        ))
    ' <<<"${audit_log}" >/dev/null
}

vap_admission_warns_for_bindings() {
  local resource_name="$1"
  local policy_name="$2"
  local first_binding_name="$3"
  local second_binding_name="$4"
  local output

  output="$(kubectl create namespace "${resource_name}" --dry-run=server 2>&1)" || return 1

  [[ "${output}" == *"ValidatingAdmissionPolicy '${policy_name}' with binding '${first_binding_name}'"* ]] &&
    [[ "${output}" == *"ValidatingAdmissionPolicy '${policy_name}' with binding '${second_binding_name}'"* ]]
}

vap_multiple_binding_audit_annotation_matches() {
  local resource_name="$1"
  local policy_name="$2"
  local evaluation_key="${policy_name}/evaluation"
  local audit_log
  audit_log="$(kube_apiserver_audit_log)" || return 1

  jq -e \
    --arg resource_name "${resource_name}" \
    --arg evaluation_key "${evaluation_key}" '
      select(.stage == "ResponseComplete" and .objectRef.name == $resource_name)
      | select(.annotations[$evaluation_key] == "true")
    ' <<<"${audit_log}" >/dev/null
}

get_ca_cert() {
  destination="$1"
  if [ $(kubectl get secret -n ${GATEKEEPER_NAMESPACE} gatekeeper-webhook-server-cert -o jsonpath='{.data.ca\.crt}' | wc -w) -eq 0 ]; then
    return 1
  fi
  kubectl get secret -n ${GATEKEEPER_NAMESPACE} gatekeeper-webhook-server-cert -o jsonpath='{.data.ca\.crt}' | base64 -d >$destination
}

constraint_enforced() {
  local kind="$1"
  local name="$2"
  local pod_list="$(kubectl -n ${GATEKEEPER_NAMESPACE} get pod -l gatekeeper.sh/operation=webhook -o json)"
  if [[ $? -ne 0 ]]; then
    echo "error gathering pods"
    return 1
  fi

  # ensure pod_count is at least one
  local pod_count=$(echo "${pod_list}" | jq '.items | length')
  if [[ ${pod_count} -lt 1 ]]; then
    echo "Gatekeeper pod count is < 1"
    return 2
  fi

  local cstr="$(kubectl get ${kind} ${name} -ojson)"
  if [[ $? -ne 0 ]]; then
    echo "Error gathering constraint ${kind} ${name}"
    return 3
  fi

  echo "checking constraint ${cstr}"

  local ready_count=$(echo "${cstr}" | jq '.metadata.generation as $generation | [.status.byPod[] | select( .operations[] == "webhook" and .observedGeneration == $generation)] | length')
  echo "ready: ${ready_count}, expected: ${pod_count}"
  [[ "${ready_count}" -eq "${pod_count}" ]]
}

mutator_enforced() {
  local kind="$1"
  local name="$2"
  local pod_list="$(kubectl -n ${GATEKEEPER_NAMESPACE} get pod -l gatekeeper.sh/operation=webhook -o json)"
  if [[ $? -ne 0 ]]; then
    echo "error gathering pods"
    return 1
  fi

  # ensure pod_count is at least one
  local pod_count=$(echo "${pod_list}" | jq '.items | length')
  if [[ ${pod_count} -lt 1 ]]; then
    echo "Gatekeeper pod count is < 1"
    return 2
  fi

  local cstr="$(kubectl get ${kind} ${name} -ojson)"
  if [[ $? -ne 0 ]]; then
    echo "Error gathering mutator ${kind} ${name}"
    return 3
  fi

  echo "checking mutator ${cstr}"

  local ready_count=$(echo "${cstr}" | jq '.metadata.generation as $generation | [.status.byPod[] | select( .operations[] == "mutation-webhook" and .observedGeneration == $generation)] | length')
  echo "ready: ${ready_count}, expected: ${pod_count}"
  [[ "${ready_count}" -eq "${pod_count}" ]]
}

total_violations() {
  local backend="$1"
  local constraint_name="pod-must-have-test"
  local constraint
  local export_logs
  constraint="$(kubectl get k8srequiredlabels "${constraint_name}" -n gatekeeper-system -ojson)" || return 1
  ct_total_violations="$(jq -r '.status.totalViolations // 0' <<<"${constraint}")" || return 1
  audit_id="$(jq -r '.status.auditTimestamp // empty' <<<"${constraint}")" || return 1
  [[ -n "${audit_id}" ]] || return 1
  violations=""
  if [[ "${backend}" == "dapr" ]]; then
    export_logs="$(kubectl logs -n fake-subscriber -l app=sub -c go-sub --tail=-1)" || return 1
    violations="$(awk -v audit_id="${audit_id}" -v constraint_name="${constraint_name}" 'index($0, "ID:\"" audit_id "\"") && index($0, "EventType:\"violation_audited\"") && index($0, "Name:\"" constraint_name "\"") {count++} END {print count+0}' <<<"${export_logs}")"
  elif [[ "${backend}" == "disk" ]]; then
    export_logs="$(kubectl logs -n gatekeeper-system -l gatekeeper.sh/operation=audit -c reader --tail=-1)" || return 1
    violations="$(awk -v audit_id="${audit_id}" -v constraint_name="${constraint_name}" 'index($0, "\"id\":\"" audit_id "\"") && index($0, "\"eventType\":\"violation_audited\"") && index($0, "\"name\":\"" constraint_name "\"") {count++} END {print count+0}' <<<"${export_logs}")"
  else
    echo "Unknown backend: ${backend}"
    return 1
  fi
  [[ "${ct_total_violations}" -eq "${violations}" ]]
}

admission_violation_count() {
  kubectl logs -n ${GATEKEEPER_NAMESPACE} -l control-plane=controller-manager -c admission-reader --tail=-1 | awk '/violation_admission/ && /denied-export-pod/ {count++} END {print count+0}'
}

admission_violation_count_greater_than() {
  local previous_count="$1"
  local current_count
  current_count="$(admission_violation_count)"
  [[ "${current_count}" -gt "${previous_count}" ]]
}

admission_export_connection_active() {
  local connection="${AUDIT_CONNECTION:-audit}"
  local ready_pods
  ready_pods="$(kubectl get pods -n "${GATEKEEPER_NAMESPACE}" -l control-plane=controller-manager -ojson | jq '[.items[] | select(.status.phase == "Running") | select(any(.status.containerStatuses[]?; .name == "manager" and .ready == true)) | .metadata.name]')"
  kubectl get connection "${connection}" -n "${GATEKEEPER_NAMESPACE}" -ojson | jq -e --argjson ready "${ready_pods}" '.metadata.generation as $generation | [.status.byPod[]? | select(.observedGeneration == $generation) | select((.operations // []) | index("webhook")) | select(((.connectionErrors // []) | length) == 0) | select(any(.publishStatuses[]?; .source == "webhook" and .active == true)) | .id] as $active | any($ready[]; . as $pod | $active | index($pod))'
}

admission_export_connection_ready() {
  local connection="${AUDIT_CONNECTION:-audit}"
  local ready_pods
  ready_pods="$(kubectl get pods -n "${GATEKEEPER_NAMESPACE}" -l control-plane=controller-manager -ojson | jq '[.items[] | select(.status.phase == "Running") | select(any(.status.containerStatuses[]?; .name == "manager" and .ready == true)) | .metadata.name]')"
  [[ "$(jq 'length' <<<"${ready_pods}")" -gt 0 ]] || return 1
  kubectl get connection "${connection}" -n "${GATEKEEPER_NAMESPACE}" -ojson | jq -e --argjson ready "${ready_pods}" '.metadata.generation as $generation | [.status.byPod[]? | select(.observedGeneration == $generation) | select((.operations // []) | index("webhook")) | select(((.connectionErrors // []) | length) == 0) | .id] as $reconciled | ($ready - $reconciled | length) == 0'
}
