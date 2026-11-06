#!/usr/bin/env bash
# Renders the Gatekeeper chart with violation export values and checks the
# audit --enable-violation-export flag, the disk Connection driver, the audit
# export volume and mount, and the audit demo sidecar.
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
