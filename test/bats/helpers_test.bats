#!/usr/bin/env bats

load helpers

jq() {
  command "${JQ_BIN:-jq}" "$@"
}

kube_apiserver_audit_log() {
  printf '%s\n' "${AUDIT_LOG}"
}

setup() {
  ADMISSION_AUDIT_ANNOTATIONS_INCLUDE_SUCCESS=true
  MATCHING_EVENTS="$(jq -cn '
    {
      schemaVersion: "v1",
      allowed: true,
      totalViolations: 0,
      includedViolations: 0,
      truncated: false,
      violations: []
    } as $allowed
    | ($allowed
        | .allowed = false
        | .totalViolations = 1
        | .includedViolations = 1
        | .violations = [{
            constraintKind: "K8sRequiredLabels",
            constraintName: "audit-constraint",
            enforcementAction: "deny"
          }]
      ) as $denied
    | (
        {"validation.gatekeeper.sh/evaluation": ($allowed | tojson)},
        {"validation.gatekeeper.sh/evaluation": ($denied | tojson)},
        {
          "gatekeeper-policy/evaluation": "true",
          "validation.policy.admission.k8s.io/validation_failure": ([
            {
              policy: "gatekeeper-policy",
              binding: "gatekeeper-policy-audit-constraint",
              validationActions: ["Deny", "Audit"]
            },
            {
              policy: "gatekeeper-policy",
              binding: "gatekeeper-policy-warn-constraint",
              validationActions: ["Warn", "Audit"]
            }
          ] | tojson)
        }
      )
    | {
        stage: "ResponseComplete",
        objectRef: {name: "audit-resource"},
        annotations: .
      }
  ')"
}

assert_audit_matchers_status() {
  local expected_status="$1"

  run webhook_admission_audit_annotation_matches audit-resource audit-constraint
  assert_equal "${expected_status}" "${status}"

  run webhook_admission_audit_annotation_without_violations_matches audit-resource
  assert_equal "${expected_status}" "${status}"

  run vap_admission_audit_annotations_match audit-resource gatekeeper-policy audit-constraint
  assert_equal "${expected_status}" "${status}"

  run vap_multiple_binding_audit_annotation_matches audit-resource gatekeeper-policy
  assert_equal "${expected_status}" "${status}"
}

@test "audit matchers find events before unrelated trailing records" {
  AUDIT_LOG="${MATCHING_EVENTS}"$'\n''{"stage":"ResponseComplete","objectRef":{"name":"unrelated"}}'

  assert_audit_matchers_status 0
}

@test "audit matchers find events after unrelated leading records" {
  AUDIT_LOG='{"stage":"ResponseComplete","objectRef":{"name":"unrelated"}}'$'\n'"${MATCHING_EVENTS}"

  assert_audit_matchers_status 0
}

@test "audit matchers reject logs with no matching resource" {
  AUDIT_LOG="$(jq -c '.objectRef.name = "other-resource"' <<<"${MATCHING_EVENTS}")"

  assert_audit_matchers_status 1
}

@test "audit matchers reject logs without annotations" {
  AUDIT_LOG="$(jq -c 'del(.annotations)' <<<"${MATCHING_EVENTS}")"

  assert_audit_matchers_status 1
}

@test "audit matchers reject empty logs" {
  AUDIT_LOG=""

  assert_audit_matchers_status 1
}

@test "violations-only VAP matchers require native failures without the marker" {
  ADMISSION_AUDIT_ANNOTATIONS_INCLUDE_SUCCESS=false
  AUDIT_LOG="$(jq -c 'del(.annotations["gatekeeper-policy/evaluation"])' <<<"${MATCHING_EVENTS}")"
  assert_audit_matchers_status 0

  AUDIT_LOG="$(jq -c 'del(.annotations["validation.policy.admission.k8s.io/validation_failure"])' <<<"${AUDIT_LOG}")"
  run vap_admission_audit_annotations_match audit-resource gatekeeper-policy audit-constraint
  assert_failure
  run vap_multiple_binding_audit_annotation_matches audit-resource gatekeeper-policy
  assert_failure
}

@test "annotation absence requires an observed successful request" {
  AUDIT_LOG='{"stage":"ResponseComplete","verb":"create","objectRef":{"name":"audit-resource","resource":"configmaps"},"responseStatus":{"code":201}}'
  run admission_audit_annotation_absent audit-resource configmaps validation.gatekeeper.sh/evaluation
  assert_success

  run admission_audit_annotation_absent other-resource configmaps validation.gatekeeper.sh/evaluation
  assert_failure

  AUDIT_LOG="$(jq -c '.annotations["validation.gatekeeper.sh/evaluation"] = "{}"' <<<"${AUDIT_LOG}")"
  run admission_audit_annotation_absent audit-resource configmaps validation.gatekeeper.sh/evaluation
  assert_failure

  AUDIT_LOG=""
  run admission_audit_annotation_absent audit-resource configmaps validation.gatekeeper.sh/evaluation
  assert_failure
}

@test "successful VAP evaluation follows the selected mode" {
  AUDIT_LOG='{"stage":"ResponseComplete","verb":"create","objectRef":{"name":"audit-resource","resource":"namespaces"},"responseStatus":{"code":201},"annotations":{"gatekeeper-policy/evaluation":"true"}}'
  run vap_admission_audit_annotation_without_violations_matches audit-resource gatekeeper-policy
  assert_success

  ADMISSION_AUDIT_ANNOTATIONS_INCLUDE_SUCCESS=false
  run vap_admission_audit_annotation_without_violations_matches audit-resource gatekeeper-policy
  assert_failure

  AUDIT_LOG="$(jq -c 'del(.annotations)' <<<"${AUDIT_LOG}")"
  run vap_admission_audit_annotation_without_violations_matches audit-resource gatekeeper-policy
  assert_success

  unset ADMISSION_AUDIT_ANNOTATIONS_INCLUDE_SUCCESS
  run vap_admission_audit_annotation_without_violations_matches audit-resource gatekeeper-policy
  assert_success

  AUDIT_LOG="$(jq -c '.annotations["gatekeeper-policy/evaluation"] = "true"' <<<"${AUDIT_LOG}")"
  run vap_admission_audit_annotation_without_violations_matches audit-resource gatekeeper-policy
  assert_failure

  AUDIT_LOG=""
  run vap_admission_audit_annotation_without_violations_matches audit-resource gatekeeper-policy
  assert_failure
}

@test "VAP readiness requires the requested native warning and denial" {
  kubectl() {
    [[ "$*" == "create namespace probe --dry-run=server" ]] || return 2
    printf '%s\n' "${mock_output}"
    return "${mock_status}"
  }
  mock_status=1
  denial="ValidatingAdmissionPolicy 'policy' with binding 'deny-binding' denied request: missing required label"
  warning="Warning: Validation failed for ValidatingAdmissionPolicy 'policy' with binding 'warn-binding': missing required label"

  mock_output="${denial}"
  run vap_admission_enforced probe policy deny-binding
  assert_success
  run vap_admission_enforced probe policy deny-binding warn-binding
  assert_failure

  mock_output="${warning}"$'\n'"${denial}"
  run vap_admission_enforced probe policy deny-binding warn-binding
  assert_success

  mock_output="Warning: Validation failed for ValidatingAdmissionPolicy 'policy' with binding 'other-binding': missing required label"$'\n'"${denial}"
  run vap_admission_enforced probe policy deny-binding warn-binding
  assert_failure

  mock_output="${warning}"$'\n''Error from server: admission webhook denied request'
  run vap_admission_enforced probe policy deny-binding warn-binding
  assert_failure

  mock_output="${warning}"$'\n'"${denial}"
  mock_status=0
  run vap_admission_enforced probe policy deny-binding warn-binding
  assert_failure
}

@test "VAP cleanup removes constraints before their template and remaining fixtures" {
  BATS_TESTS_DIR=fixtures
  calls="${BATS_TEST_TMPDIR}/kubectl-calls"
  kubectl() {
    printf '%s\n' "$*" >>"${calls}"
    if [[ "$1" == get ]]; then
      echo "customresourcedefinition.apiextensions.k8s.io/k8srequiredlabelsvap.constraints.gatekeeper.sh"
    fi
  }

  run cleanup_vap_test
  assert_success
  expected="get crd k8srequiredlabelsvap.constraints.gatekeeper.sh --ignore-not-found -o name
delete --ignore-not-found --timeout=60s -f fixtures/constraints/all_ns_must_have_label_provided_vapbinding.yaml -f fixtures/constraints/all_ns_must_have_label_provided_vapbinding_scoped.yaml
delete --ignore-not-found --timeout=60s -f fixtures/templates/k8srequiredlabels_template_vap.yaml
delete --ignore-not-found --timeout=60s -f fixtures/good/good_ns.yaml -f fixtures/bad/bad_ns.yaml
delete pod temp --ignore-not-found --timeout=60s"
  assert_equal "${expected}" "$(<"${calls}")"
}

@test "VAP cleanup skips constraint deletion after its CRD is gone" {
  BATS_TESTS_DIR=fixtures
  calls="${BATS_TEST_TMPDIR}/kubectl-calls"
  kubectl() {
    printf '%s\n' "$*" >>"${calls}"
  }

  run cleanup_vap_test
  assert_success
  assert_not_match 'fixtures/constraints/' "$(<"${calls}")"
  assert_match 'fixtures/templates/k8srequiredlabels_template_vap.yaml' "$(<"${calls}")"
  assert_match 'delete pod temp' "$(<"${calls}")"
}

@test "VAP cleanup continues and reports a constraint deletion failure" {
  BATS_TESTS_DIR=fixtures
  calls="${BATS_TEST_TMPDIR}/kubectl-calls"
  kubectl() {
    printf '%s\n' "$*" >>"${calls}"
    if [[ "$1" == get ]]; then
      echo "customresourcedefinition.apiextensions.k8s.io/k8srequiredlabelsvap.constraints.gatekeeper.sh"
    elif [[ "$*" == *"fixtures/constraints/"* ]]; then
      echo "constraint deletion failed" >&2
      return 1
    fi
  }

  run cleanup_vap_test
  assert_failure
  assert_match 'constraint deletion failed' "${output}"
  assert_match 'fixtures/templates/k8srequiredlabels_template_vap.yaml' "$(<"${calls}")"
  assert_match 'fixtures/good/good_ns.yaml' "$(<"${calls}")"
  assert_match 'delete pod temp' "$(<"${calls}")"
}

@test "VAP cleanup reports discovery errors and still attempts remaining cleanup" {
  BATS_TESTS_DIR=fixtures
  calls="${BATS_TEST_TMPDIR}/kubectl-calls"
  kubectl() {
    printf '%s\n' "$*" >>"${calls}"
    if [[ "$1" == get ]]; then
      echo "Forbidden" >&2
      return 1
    fi
  }

  run cleanup_vap_test
  assert_failure
  assert_match 'Forbidden' "${output}"
  assert_not_match 'fixtures/constraints/' "$(<"${calls}")"
  assert_match 'fixtures/templates/k8srequiredlabels_template_vap.yaml' "$(<"${calls}")"
  assert_match 'delete pod temp' "$(<"${calls}")"
}

@test "audit export mode follows the deployed manager flags" {
  GATEKEEPER_NAMESPACE=gatekeeper-system
  kubectl() { printf '%s\n' "${mock_deployment}"; }
  local test_case expected args
  for test_case in \
    'true|[]' \
    'true|["--audit-interval=60"]' \
    'true|["--enable-violation-export"]' \
    'true|["--enable-violation-export=1"]' \
    'false|["--enable-violation-export=false"]' \
    'false|["-enable-violation-export=0"]' \
    'false|["--enable-violation-export=FALSE"]' \
    'false|["--enable-violation-export=true","--enable-violation-export=false"]' \
    'true|["--enable-violation-export=false","--enable-violation-export=true"]'; do
    expected="${test_case%%|*}"
    args="${test_case#*|}"
    mock_deployment="$(jq -cn --argjson args "${args}" '{
      spec: {template: {spec: {containers: [
        {name: "manager", args: $args},
        {name: "reader", args: ["--enable-violation-export=true"]}
      ]}}}
    }')"
    run audit_export_enabled
    assert_success
    assert_equal "${expected}" "${output}"
  done
}

@test "audit export mode rejects invalid flags and missing managers" {
  GATEKEEPER_NAMESPACE=gatekeeper-system
  kubectl() { printf '%s\n' "${mock_deployment}"; }
  local value
  for value in "" invalid false=ignored; do
    mock_deployment="$(jq -cn --arg value "${value}" '{
      spec: {template: {spec: {containers: [
        {name: "manager", args: [("--enable-violation-export=" + $value)]}
      ]}}}
    }')"
    run audit_export_enabled
    assert_failure
    assert_match 'invalid audit export flag value' "${output}"
  done
  mock_deployment='{"spec":{"template":{"spec":{"containers":[]}}}}'
  run audit_export_enabled
  assert_failure
  assert_match 'expected one audit manager container' "${output}"
}

@test "audit export mode reports deployment lookup errors" {
  GATEKEEPER_NAMESPACE=gatekeeper-system
  kubectl() { echo "deployment lookup forbidden" >&2; return 1; }
  run audit_export_enabled
  assert_failure
  assert_match 'deployment lookup forbidden' "${output}"
}

@test "fresh audit proof requires matching constraint status, violation and completion" {
  mock_constraint='{"status":{"auditTimestamp":"2026-11-04T20:00:02Z","violations":[{"name":"test-resource"}]}}'
  mock_audit_logs='startup output before JSON logging
{"event_type":"violation_audited","audit_id":"2026-11-04T20:00:02Z","constraint_name":"test-constraint","resource_name":"test-resource"}
{"event_type":"audit_finished","audit_id":"2026-11-04T20:00:02Z"}'
  kubectl() { printf '%s\n' "${mock_constraint}"; }
  audit_logs() { printf '%s\n' "${mock_audit_logs}"; }

  run audit_constraint_run_finished k8srequiredlabels test-constraint test-resource 2026-11-04T20:00:01Z
  assert_success
  assert_equal "2026-11-04T20:00:02Z" "${output}"

  run audit_constraint_run_finished k8srequiredlabels test-constraint test-resource 2026-11-04T20:00:02Z
  assert_failure
  run audit_constraint_run_finished k8srequiredlabels other-constraint test-resource 2026-11-04T20:00:01Z
  assert_failure
  run audit_constraint_run_finished k8srequiredlabels test-constraint other-resource 2026-11-04T20:00:01Z
  assert_failure

  mock_constraint='{"status":{"auditTimestamp":"2026-11-04T20:00:02Z","violations":[]}}'
  run audit_constraint_run_finished k8srequiredlabels test-constraint test-resource 2026-11-04T20:00:01Z
  assert_failure
}

@test "fresh audit proof rejects historical, unrelated and incomplete runs" {
  mock_constraint='{"status":{"auditTimestamp":"2026-11-04T20:00:02Z","violations":[{"name":"test-resource"}]}}'
  kubectl() { printf '%s\n' "${mock_constraint}"; }
  audit_logs() { printf '%s\n' "${mock_audit_logs}"; }

  mock_audit_logs='{"event_type":"audit_finished","audit_id":"2026-11-04T20:00:00Z"}
{"event_type":"violation_audited","audit_id":"2026-11-04T20:00:00Z","constraint_name":"test-constraint","resource_name":"test-resource"}
{"event_type":"audit_finished","audit_id":"2026-11-04T20:00:02Z"}'
  run audit_constraint_run_finished k8srequiredlabels test-constraint test-resource 2026-11-04T20:00:01Z
  assert_failure

  mock_audit_logs='{"event_type":"violation_audited","audit_id":"2026-11-04T20:00:02Z","constraint_name":"test-constraint","resource_name":"test-resource"}
{"event_type":"audit_finished","audit_id":"2026-11-04T20:00:03Z"}'
  run audit_constraint_run_finished k8srequiredlabels test-constraint test-resource 2026-11-04T20:00:01Z
  assert_failure

  mock_audit_logs='{"event_type":"violation_audited","audit_id":"2026-11-04T20:00:02Z","constraint_name":"test-constraint","resource_name":"test-resource"}
{"event_type":"other","msg":"audit_finished","audit_id":"2026-11-04T20:00:02Z"}'
  run audit_constraint_run_finished k8srequiredlabels test-constraint test-resource 2026-11-04T20:00:01Z
  assert_failure
}

@test "fresh audit proof surfaces resource and log lookup errors" {
  kubectl() { echo "constraint lookup forbidden" >&2; return 1; }
  run audit_constraint_run_finished k8srequiredlabels test-constraint test-resource ""
  assert_failure
  assert_match 'constraint lookup forbidden' "${output}"

  kubectl() { echo '{"status":{}}'; }
  audit_logs() { echo "audit logs unavailable" >&2; return 1; }
  run audit_constraint_run_finished k8srequiredlabels test-constraint test-resource ""
  assert_failure
  assert_match 'audit logs unavailable' "${output}"
}

@test "audit log records support JSON and DEBUG console output without inventing evidence" {
  audit_logs() {
    printf '%s\n' 'startup output' '{"audit_id":"2026-11-04T20:00:01Z","event_type":"audit_started"}' \
      'null' '123' '"text"'
    printf '2026-11-04T20:00:01Z\tinfo\tcontroller\tmessage {not JSON}\t{"audit_id":"2026-11-04T20:00:01Z","event_type":"audit_finished"}\n'
  }
  run audit_log_records
  assert_success
  run jq -se 'length == 2 and .[0].event_type == "audit_started" and .[1].event_type == "audit_finished"' <<<"${output}"
  assert_success
}
