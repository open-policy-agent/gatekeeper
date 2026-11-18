#!/usr/bin/env bats

load helpers

BATS_TESTS_DIR=${BATS_TESTS_DIR:-test/bats/tests}
WAIT_TIME=120
SLEEP_TIME=1
CLEAN_CMD="echo cleaning..."
GATEKEEPER_NAMESPACE=${GATEKEEPER_NAMESPACE:-gatekeeper-system}

setup_file() {
  if [[ -z "${ENABLE_VAP_TESTS:-}" ]]; then
    skip "skipping VAP tests"
  fi
  local resources
  resources="$(kubectl api-resources --api-group=admissionregistration.k8s.io -o name)" || return 1
  if ! grep -qx 'validatingadmissionpolicies.admissionregistration.k8s.io' <<<"${resources}"; then
    skip "VAP is not enabled for the cluster"
  fi
  kubectl -n "${GATEKEEPER_NAMESPACE}" rollout status deployment/gatekeeper-controller-manager --timeout=120s
  kubectl -n "${GATEKEEPER_NAMESPACE}" rollout status deployment/gatekeeper-audit --timeout=120s
}

teardown() {
  bash -c "${CLEAN_CMD}"
}

@test "generated VAP enforces without a validating webhook and keeps Config exclusions" {
  local namespace="vap-webhook-disabled"
  local excluded_namespace="vap-webhook-disabled-excluded"
  local constraint_name="vap-webhook-disabled"
  local policy_name="gatekeeper-k8srequiredlabelsvap"
  local binding_name="${policy_name}-${constraint_name}"
  local webhook_name="gatekeeper-validating-webhook-configuration"
  local snapshot_dir
  snapshot_dir="$(mktemp -d)"
  CLEAN_CMD="rm -rf '${snapshot_dir}'; ${CLEAN_CMD}"

  local deployment
  for deployment in gatekeeper-controller-manager gatekeeper-audit; do
    kubectl -n "${GATEKEEPER_NAMESPACE}" get deployment "${deployment}" -o json |
      jq '{spec:{template:{spec:{containers:[.spec.template.spec.containers[] | select(.name == "manager") | {name,args}]}}}}' > "${snapshot_dir}/${deployment}.json"
    CLEAN_CMD="kubectl -n '${GATEKEEPER_NAMESPACE}' patch deployment '${deployment}' --type=strategic --patch-file '${snapshot_dir}/${deployment}.json'; kubectl -n '${GATEKEEPER_NAMESPACE}' rollout status deployment '${deployment}' --timeout=180s; ${CLEAN_CMD}"
    jq '.spec.template.spec.containers[0].args |= (map(select(startswith("--validating-webhook-configuration-name=") | not)) + ["--validating-webhook-configuration-name="])' "${snapshot_dir}/${deployment}.json" > "${snapshot_dir}/${deployment}-updated.json"
    kubectl -n "${GATEKEEPER_NAMESPACE}" patch deployment "${deployment}" --type=strategic --patch-file "${snapshot_dir}/${deployment}-updated.json"
    kubectl -n "${GATEKEEPER_NAMESPACE}" rollout status deployment "${deployment}" --timeout=180s
  done

  kubectl get validatingwebhookconfiguration "${webhook_name}" -o json |
    jq 'del(.metadata.uid, .metadata.resourceVersion, .metadata.creationTimestamp, .metadata.managedFields)' > "${snapshot_dir}/webhook.json"
  CLEAN_CMD="kubectl apply -f '${snapshot_dir}/webhook.json'; ${CLEAN_CMD}"
  kubectl delete validatingwebhookconfiguration "${webhook_name}"
  run kubectl get validatingwebhookconfiguration "${webhook_name}"
  assert_failure
  assert_match 'NotFound' "${output}"

  if kubectl -n "${GATEKEEPER_NAMESPACE}" get config.config.gatekeeper.sh config -o json > "${snapshot_dir}/config.json"; then
    jq 'del(.metadata.uid, .metadata.resourceVersion, .metadata.creationTimestamp, .metadata.managedFields, .status)' "${snapshot_dir}/config.json" > "${snapshot_dir}/config-restore.json"
    CLEAN_CMD="kubectl apply -f '${snapshot_dir}/config-restore.json'; ${CLEAN_CMD}"
  else
    kubectl create --dry-run=client -f "${BATS_TESTS_DIR}/sync_with_exclusion_exact_match.yaml" -o json > "${snapshot_dir}/config.json"
    CLEAN_CMD="kubectl -n '${GATEKEEPER_NAMESPACE}' delete config.config.gatekeeper.sh config --ignore-not-found; ${CLEAN_CMD}"
  fi
  jq --arg namespace "${GATEKEEPER_NAMESPACE}" --arg excluded "${excluded_namespace}" '.metadata.namespace = $namespace | .spec.match += [{excludedNamespaces:[$excluded],processes:["webhook"]}]' "${snapshot_dir}/config.json" | kubectl apply -f -

  CLEAN_CMD="kubectl delete k8srequiredlabelsvap.constraints.gatekeeper.sh '${constraint_name}' --ignore-not-found; kubectl delete constrainttemplate k8srequiredlabelsvap --ignore-not-found; kubectl delete namespace '${namespace}' '${excluded_namespace}' --ignore-not-found; ${CLEAN_CMD}"
  kubectl create namespace "${namespace}"
  kubectl create namespace "${excluded_namespace}"
  kubectl label namespace "${namespace}" admission.gatekeeper.sh/ignore=unprotected-label
  kubectl apply -f "${BATS_TESTS_DIR}/templates/k8srequiredlabels_template_vap.yaml"
  wait_for_process "${WAIT_TIME}" "${SLEEP_TIME}" "kubectl get crd k8srequiredlabelsvap.constraints.gatekeeper.sh"
  kubectl create --dry-run=client -f "${BATS_TESTS_DIR}/constraints/admission_audit_annotations_vap.yaml" -o json |
    jq --arg name "${constraint_name}" --arg namespace "${namespace}" --arg excluded "${excluded_namespace}" '.metadata.name = $name | .spec.match = {kinds:[{apiGroups:[""],kinds:["ConfigMap"]}],namespaces:[$namespace,$excluded]}' |
    kubectl apply -f -
  wait_for_process "${WAIT_TIME}" "${SLEEP_TIME}" "vap_configmap_admission_enforced '${namespace}' '${policy_name}' '${binding_name}'"
  run kubectl create configmap vap-exemption-probe --namespace "${namespace}" --dry-run=server
  assert_failure
  assert_match "ValidatingAdmissionPolicy '${policy_name}' with binding '${binding_name}' denied request" "${output}"
  assert_match 'missing required label' "${output}"
  wait_for_process "${WAIT_TIME}" "${SLEEP_TIME}" "kubectl create configmap vap-exemption-probe --namespace '${excluded_namespace}' --dry-run=server"
  run bash -c "kubectl get validatingadmissionpolicy '${policy_name}' -o json | jq -e '(.spec.matchConstraints.namespaceSelector // {}) == {}'"
  assert_success
}

@test "generated VAP namespace exemptions require the ignore label" {
  local constraint_name="vap-namespace-exemptions"
  local policy_name="gatekeeper-k8srequiredlabelsvap"
  local binding_name="${policy_name}-${constraint_name}"
  local namespaces=(vap-exempt-exact vap-exempt-prefix-test vap-exempt-test-suffix)
  local control_namespace="vap-nonexempt-control"
  local snapshot_dir
  snapshot_dir="$(mktemp -d)"
  CLEAN_CMD="rm -rf '${snapshot_dir}'; ${CLEAN_CMD}"

  local deployment
  for deployment in gatekeeper-controller-manager gatekeeper-audit; do
    kubectl -n "${GATEKEEPER_NAMESPACE}" get deployment "${deployment}" -o json |
      jq '{spec:{template:{spec:{containers:[.spec.template.spec.containers[] | select(.name == "manager") | {name,args}]}}}}' > "${snapshot_dir}/${deployment}.json"
    CLEAN_CMD="kubectl -n '${GATEKEEPER_NAMESPACE}' patch deployment '${deployment}' --type=strategic --patch-file '${snapshot_dir}/${deployment}.json'; kubectl -n '${GATEKEEPER_NAMESPACE}' rollout status deployment '${deployment}' --timeout=180s; ${CLEAN_CMD}"
    jq '.spec.template.spec.containers[0].args += ["--exempt-namespace=vap-exempt-exact", "--exempt-namespace-prefix=vap-exempt-prefix-", "--exempt-namespace-suffix=-suffix"]' "${snapshot_dir}/${deployment}.json" > "${snapshot_dir}/${deployment}-updated.json"
    kubectl -n "${GATEKEEPER_NAMESPACE}" patch deployment "${deployment}" --type=strategic --patch-file "${snapshot_dir}/${deployment}-updated.json"
    kubectl -n "${GATEKEEPER_NAMESPACE}" rollout status deployment "${deployment}" --timeout=180s
  done

  CLEAN_CMD="kubectl delete k8srequiredlabelsvap.constraints.gatekeeper.sh '${constraint_name}' --ignore-not-found; kubectl delete constrainttemplate k8srequiredlabelsvap --ignore-not-found; kubectl delete namespace ${namespaces[*]} '${control_namespace}' --ignore-not-found; ${CLEAN_CMD}"
  local namespace
  for namespace in "${namespaces[@]}" "${control_namespace}"; do
    kubectl create namespace "${namespace}"
  done

  kubectl apply -f "${BATS_TESTS_DIR}/templates/k8srequiredlabels_template_vap.yaml"
  wait_for_process "${WAIT_TIME}" "${SLEEP_TIME}" "kubectl get validatingadmissionpolicy '${policy_name}'"
  wait_for_process "${WAIT_TIME}" "${SLEEP_TIME}" "kubectl get crd k8srequiredlabelsvap.constraints.gatekeeper.sh"
  kubectl create --dry-run=client -f "${BATS_TESTS_DIR}/constraints/admission_audit_annotations_vap.yaml" -o json |
    jq --arg name "${constraint_name}" '.metadata.name = $name | .spec.match = {kinds:[{apiGroups:[""],kinds:["ConfigMap"]}],namespaces:["vap-exempt-exact","vap-exempt-prefix-test","vap-exempt-test-suffix","vap-nonexempt-control"]}' |
    kubectl apply -f -
  wait_for_process "${WAIT_TIME}" "${SLEEP_TIME}" "kubectl get validatingadmissionpolicybinding '${binding_name}'"
  wait_for_process "${WAIT_TIME}" "${SLEEP_TIME}" "vap_configmap_admission_enforced '${control_namespace}' '${policy_name}' '${binding_name}'"

  for namespace in "${namespaces[@]}"; do
    run kubectl create configmap vap-exemption-probe --namespace "${namespace}" --dry-run=server
    assert_failure
    assert_match "ValidatingAdmissionPolicy '${policy_name}' with binding '${binding_name}' denied request" "${output}"
    assert_match 'missing required label' "${output}"

    kubectl label namespace "${namespace}" admission.gatekeeper.sh/ignore=exemption-test
    wait_for_process "${WAIT_TIME}" "${SLEEP_TIME}" "kubectl create configmap vap-exemption-probe --namespace '${namespace}' --dry-run=server"
    kubectl -n "${namespace}" create configmap existing-violation --from-literal=state=before

    kubectl label namespace "${namespace}" admission.gatekeeper.sh/ignore-
    wait_for_process "${WAIT_TIME}" "${SLEEP_TIME}" "vap_configmap_admission_enforced '${namespace}' '${policy_name}' '${binding_name}'"

    run kubectl -n "${namespace}" patch configmap existing-violation --type=merge -p '{"data":{"state":"after"}}' --dry-run=server
    assert_failure
    assert_match "${policy_name}" "${output}"
    assert_match 'missing required label' "${output}"
  done
}
