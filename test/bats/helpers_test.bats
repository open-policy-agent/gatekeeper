#!/usr/bin/env bats

load helpers

jq() {
  command "${JQ_BIN:-jq}" "$@"
}

kube_apiserver_audit_log() {
  printf '%s\n' "${AUDIT_LOG}"
}

setup() {
  ADMISSION_AUDIT_ANNOTATIONS_VIOLATIONS_ONLY=false
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
  ADMISSION_AUDIT_ANNOTATIONS_VIOLATIONS_ONLY=true
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

  ADMISSION_AUDIT_ANNOTATIONS_VIOLATIONS_ONLY=true
  run vap_admission_audit_annotation_without_violations_matches audit-resource gatekeeper-policy
  assert_failure

  AUDIT_LOG="$(jq -c 'del(.annotations)' <<<"${AUDIT_LOG}")"
  run vap_admission_audit_annotation_without_violations_matches audit-resource gatekeeper-policy
  assert_success

  AUDIT_LOG=""
  run vap_admission_audit_annotation_without_violations_matches audit-resource gatekeeper-policy
  assert_failure
}
