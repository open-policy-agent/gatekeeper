#!/usr/bin/env bash
# Renders the Gatekeeper chart and checks optional audit-trigger arguments and
# RBAC, violation export arguments, the disk Connection driver, the audit export
# volume and mount, and the audit demo sidecar.
# Requires helm, yq (v4) and jq. Set HELM or CHART to override the defaults.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HELM="${HELM:-helm}"
CHART="${CHART:-${ROOT}/manifest_staging/charts/gatekeeper}"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
printf 'enableViolationExport: null\n' > "${tmp}/null.yaml"

failures=0

# check NAME FLAG VOLUME DRIVER SIDECAR [HELM_ARGS...]
# FLAG is the rendered flag value or "omitted"; VOLUME is "yes" when both the
# volume and the manager mount are rendered; DRIVER is the Connection driver
# or "none"; SIDECAR is "yes" when the reader container is rendered.
check() {
  local name="$1" want="$2|$3|$4|$5" got
  shift 5
  got="$("${HELM}" template gatekeeper "${CHART}" "$@" | yq -o=json -I=0 '.' - | jq -rs '
    map(select(. != null)) as $docs
    | ($docs[] | select(.kind == "Deployment" and .metadata.name == "gatekeeper-audit") | .spec.template.spec) as $pod
    | ($pod.containers[] | select(.name == "manager")) as $manager
    | ([$manager.args[] | select(startswith("--enable-violation-export=")) | ltrimstr("--enable-violation-export=")] | first // "omitted") as $flag
    | (($pod.volumes // []) | any(.name == "tmp-violations")) as $volume
    | (($manager.volumeMounts // []) | any(.name == "tmp-violations")) as $mount
    | ([$docs[] | select(.kind == "Connection") | .spec.driver] | first // "none") as $driver
    | ($pod.containers | any(.name == "reader")) as $sidecar
    | [$flag,
       (if $volume != $mount then "mismatch" elif $volume then "yes" else "no" end),
       $driver,
       (if $sidecar then "yes" else "no" end)]
    | join("|")')"
  if [[ "${got}" == "${want}" ]]; then
    echo "PASS: ${name}"
  else
    echo "FAIL: ${name}: want ${want}, got ${got}"
    failures=$((failures + 1))
  fi
}

reject_dapr() {
  local name="$1" output
  shift
  if output="$("${HELM}" template gatekeeper "${CHART}" "$@" 2>&1)"; then
    echo "FAIL: ${name}: expected the removed Dapr backend to be rejected"
    failures=$((failures + 1))
  elif [[ "${output}" == *"exportBackend dapr is no longer supported"* ||
          "${output}" == *"enableAdmissionViolationExport requires exportBackend to be disk"* ]]; then
    echo "PASS: ${name}"
  else
    echo "FAIL: ${name}: unexpected render error: ${output}"
    failures=$((failures + 1))
  fi
}

check_audit_trigger() {
  local name="$1" want="$2|$3" got
  shift 3
  got="$("${HELM}" template gatekeeper "${CHART}" "$@" | yq -o=json -I=0 '.' - | jq -rs '
    map(select(. != null)) as $docs
    | ([$docs[] | select(.kind == "Deployment" and .metadata.name == "gatekeeper-audit")
        | .spec.template.spec.containers[] | select(.name == "manager") | .args[]
        | select(startswith("--enable-audit-trigger=")) | ltrimstr("--enable-audit-trigger=")]
       | first // "omitted") as $flag
    | ([$docs[] | select(.kind == "ClusterRole" and .metadata.name == "gatekeeper-manager-role")
        | .rules[] | select((.apiGroups | index("audit.gatekeeper.sh")) and
                           (.resources | index("audittriggers/status")))
        | .verbs[] | select(. == "patch" or . == "update")] | length > 0) as $writes
    | [$flag, (if $writes then "yes" else "no" end)] | join("|")')"
  if [[ "${got}" == "${want}" ]]; then
    echo "PASS: ${name}"
  else
    echo "FAIL: ${name}: want ${want}, got ${got}"
    failures=$((failures + 1))
  fi
}

check_audit_trigger "audit trigger defaults" omitted no
check_audit_trigger "audit trigger false"    false   no  --set enableAuditTrigger=false
check_audit_trigger "audit trigger true"     true    yes --set enableAuditTrigger=true
check_audit_trigger "audit trigger string false" false no --set-string enableAuditTrigger=false
check_audit_trigger "audit trigger string zero" false no --set-string enableAuditTrigger=0
check_audit_trigger "audit trigger string true" true yes --set-string enableAuditTrigger=true
check_audit_trigger "audit trigger null" omitted no --set enableAuditTrigger=null
check_audit_trigger "audit trigger empty string" omitted no --set-string enableAuditTrigger=
check_audit_trigger "audit disabled"         omitted no  --set enableAuditTrigger=true --set disableAudit=true
# The staged static Deployment can pin an older binary that does not know this flag.
yq -o=json -I=0 '.' "${ROOT}/manifest_staging/deploy/gatekeeper.yaml" |
  jq -se '[.[] | select(.kind=="Deployment" and .metadata.name=="gatekeeper-audit") |
    .spec.template.spec.containers[] | select(.name=="manager")] |
    length==1 and all(.[0].args[]; startswith("--enable-audit-trigger") | not)' >/dev/null
echo "PASS: default static audit deployment omits the trigger flag"
if output="$("${HELM}" template gatekeeper "${CHART}" --set-string enableAuditTrigger=invalid 2>&1)"; then
  echo "FAIL: invalid audit trigger value accepted"
  failures=$((failures + 1))
elif [[ "${output}" == *"enableAuditTrigger must be a boolean"* ]]; then
  echo "PASS: invalid audit trigger value rejected"
else
  echo "FAIL: unexpected invalid audit trigger render error: ${output}"
  failures=$((failures + 1))
fi

#     name                                  flag     volume driver sidecar args
check "defaults"                            omitted  no     none   no
check "null"                                omitted  no     none   no      -f "${tmp}/null.yaml"
check "empty string"                        omitted  no     none   no      --set-string enableViolationExport=
check "false"                               false    no     none   no      --set enableViolationExport=false
check "string false"                        false    no     none   no      --set-string enableViolationExport=false
check "string false, disk"                  false    no     none   no      --set-string enableViolationExport=false --set exportBackend=disk
check "disk only"                           omitted  no     none   no      --set exportBackend=disk
check "admission-only disk"                 omitted  yes    disk   no      --set enableAdmissionViolationExport=true --set exportBackend=disk
check "admission-only disk, audit false"    false    no     disk   no      --set enableAdmissionViolationExport=true --set exportBackend=disk --set enableViolationExport=false
check "admission string false, disk"        omitted  no     none   no      --set-string enableAdmissionViolationExport=false --set exportBackend=disk
check "true, disk"                          true     yes    disk   yes     --set enableViolationExport=true --set exportBackend=disk
check "combined, disk"                      true     yes    disk   yes     --set enableViolationExport=true --set enableAdmissionViolationExport=true --set exportBackend=disk
check "disabled, dapr"                      false    no     none   no      --set enableViolationExport=false --set enableAdmissionViolationExport=false --set exportBackend=dapr
check "string false, dapr"                  false    no     none   no      --set-string enableViolationExport=false --set-string enableAdmissionViolationExport=false --set exportBackend=dapr
reject_dapr "default audit, dapr" --set exportBackend=dapr
reject_dapr "true, dapr" --set enableViolationExport=true --set exportBackend=dapr
reject_dapr "admission, dapr" --set enableViolationExport=false --set enableAdmissionViolationExport=true --set exportBackend=dapr

if [[ "${failures}" -ne 0 ]]; then
  echo "${failures} helm render check(s) failed" >&2
  exit 1
fi
echo "all helm render checks passed"
