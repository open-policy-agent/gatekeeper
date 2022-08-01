#!/usr/bin/env bats

load helpers

BATS_TESTS_DIR=${BATS_TESTS_DIR:-test/bats/tests}
WAIT_TIME=120
SLEEP_TIME=1
CLEAN_CMD="echo cleaning..."
GATEKEEPER_NAMESPACE=${GATEKEEPER_NAMESPACE:-gatekeeper-system}

teardown() {
  bash -c "${CLEAN_CMD}"
}

teardown_file() {
  kubectl label ns ${GATEKEEPER_NAMESPACE} admission.gatekeeper.sh/ignore=no-self-managing --overwrite || true
  kubectl delete ns \
    gatekeeper-test-playground \
    gatekeeper-excluded-namespace \
    gatekeeper-excluded-prefix-match-namespace \
    gatekeeper-excluded-suffix-match-namespace || true
  kubectl delete "$(kubectl api-resources --api-group=constraints.gatekeeper.sh -o name | tr "\n" "," | sed -e 's/,$//')" -l gatekeeper.sh/tests=yes || true
  kubectl delete ConstraintTemplates -l gatekeeper.sh/tests=yes || true
  kubectl delete configs.config.gatekeeper.sh -n ${GATEKEEPER_NAMESPACE} -l gatekeeper.sh/tests=yes || true
}

@test "gatekeeper-controller-manager is running" {
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl -n ${GATEKEEPER_NAMESPACE} wait --for=condition=Ready --timeout=60s pod -l control-plane=controller-manager"
}

@test "gatekeeper-audit is running" {
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl -n ${GATEKEEPER_NAMESPACE} wait --for=condition=Ready --timeout=60s pod -l control-plane=audit-controller"
}

@test "namespace label webhook is serving" {
  cert=$(mktemp)
  CLEAN_CMD="${CLEAN_CMD}; rm ${cert}"
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "get_ca_cert ${cert}"

  kubectl run temp --image=curlimages/curl -- tail -f /dev/null
  kubectl wait --for=condition=Ready --timeout=60s pod temp
  kubectl cp ${cert} temp:/tmp/cacert

  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl exec -it temp -- curl -f --cacert /tmp/cacert --connect-timeout 1 --max-time 2  https://gatekeeper-webhook-service.${GATEKEEPER_NAMESPACE}.svc:443/v1/admitlabel"
  kubectl delete pod temp
}

@test "constrainttemplates crd is established" {
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl wait --for condition=established --timeout=60s crd/constrainttemplates.templates.gatekeeper.sh"
}

@test "mutation crds are established" {
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl wait --for condition=established --timeout=60s crd/assign.mutations.gatekeeper.sh"
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl wait --for condition=established --timeout=60s crd/assignmetadata.mutations.gatekeeper.sh"
}

@test "waiting for validating webhook" {
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl get validatingwebhookconfigurations.admissionregistration.k8s.io gatekeeper-validating-webhook-configuration"
}

@test "gatekeeper mutation test" {
  kubectl apply -f ${BATS_TESTS_DIR}/mutations/k8sownerlabel_assignmetadata.yaml
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "mutator_enforced AssignMetadata k8sownerlabel"
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl apply -f ${BATS_TESTS_DIR}/mutations/mutate_cm.yaml"
  run kubectl get cm mutate-cm -o jsonpath="{.metadata.labels.owner}"
  assert_equal 'gatekeeper' "${output}"
  run kubectl get cm mutate-cm -o jsonpath="{.metadata.annotations.gatekeeper\.sh\/mutation\-id}"
  # uuid has a length of 36
  assert_len 36 "${output}"
  run kubectl get cm mutate-cm -o jsonpath="{.metadata.annotations.gatekeeper\.sh\/mutations}"
  assert_equal 'AssignMetadata//k8sownerlabel:1' "${output}"

  kubectl delete --ignore-not-found cm mutate-cm

  kubectl apply -f ${BATS_TESTS_DIR}/mutations/k8sexternalip_assign.yaml
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "mutator_enforced Assign k8sexternalip"
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl apply -f ${BATS_TESTS_DIR}/mutations/mutate_svc.yaml"
  run kubectl get svc mutate-svc -o jsonpath="{.spec.externalIPs}"
  assert_equal "" "${output}"
  run kubectl get svc mutate-svc -o jsonpath="{.metadata.annotations.gatekeeper\.sh\/mutation\-id}"
  assert_len 36 "${output}"
  run kubectl get svc mutate-svc -o jsonpath="{.metadata.annotations.gatekeeper\.sh\/mutations}"
  assert_equal 'Assign//k8sexternalip:1' "${output}"

  kubectl delete --ignore-not-found svc mutate-svc
  kubectl delete --ignore-not-found assignmetadata k8sownerlabel
  kubectl delete --ignore-not-found assign k8sexternalip
}

@test "applying sync config" {
  kubectl apply -n ${GATEKEEPER_NAMESPACE} -f ${BATS_TESTS_DIR}/sync.yaml
}

# creating namespaces and audit constraints early so they will have time to reconcile
@test "create basic resources" {
  kubectl create ns gatekeeper-excluded-namespace
  kubectl create ns gatekeeper-excluded-prefix-match-namespace
  kubectl create ns gatekeeper-excluded-suffix-match-namespace
  kubectl apply -f ${BATS_TESTS_DIR}/good/playground_ns.yaml
  kubectl apply -f ${BATS_TESTS_DIR}/good/no_dupe_cm.yaml
  kubectl apply -f ${BATS_TESTS_DIR}/bad/bad_cm_audit.yaml

  kubectl apply -f ${BATS_TESTS_DIR}/templates/k8srequiredlabels_template.yaml
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl apply -f ${BATS_TESTS_DIR}/constraints/all_cm_must_have_gatekeeper_audit.yaml"
}

@test "no ignore label unless namespace is exempt test" {
  run kubectl apply -f ${BATS_TESTS_DIR}/bad/ignore_label_ns.yaml
  assert_match 'Only exempt namespace can have the admission.gatekeeper.sh/ignore label' "${output}"
  assert_failure
}

@test "gatekeeper ns ignore label can be patched" {
  kubectl patch ns ${GATEKEEPER_NAMESPACE} --type=json -p='[{"op": "replace", "path": "/metadata/labels/admission.gatekeeper.sh~1ignore", "value": "ignore-label-test-passed"}]'
}

@test "required labels warn and dryrun test" {
  kubectl apply -f ${BATS_TESTS_DIR}/constraints/all_cm_must_have_gatekeeper.yaml
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "constraint_enforced k8srequiredlabels cm-must-have-gk"

  kubectl apply -f ${BATS_TESTS_DIR}/good/good_cm.yaml

  run kubectl apply -f ${BATS_TESTS_DIR}/bad/bad_cm.yaml
  assert_match 'denied the request' "${output}"
  assert_failure

  kubectl apply -f ${BATS_TESTS_DIR}/constraints/all_cm_must_have_gatekeeper-warn.yaml
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "constraint_enforced k8srequiredlabels cm-must-have-gk"

  # deploying a violation with warn enforcement action will be accepted
  run kubectl apply -f ${BATS_TESTS_DIR}/bad/bad_cm.yaml
  assert_match 'Warning' "${output}"
  assert_success

  kubectl delete --ignore-not-found -f ${BATS_TESTS_DIR}/bad/bad_cm.yaml

  kubectl apply -f ${BATS_TESTS_DIR}/constraints/all_cm_must_have_gatekeeper-dryrun.yaml
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "constraint_enforced k8srequiredlabels cm-must-have-gk"

  # deploying a violation with dryrun enforcement action will be accepted
  kubectl apply -f ${BATS_TESTS_DIR}/bad/bad_cm.yaml

  kubectl delete --ignore-not-found -f ${BATS_TESTS_DIR}/bad/bad_cm.yaml
}

@test "container limits test" {
  kubectl apply -f ${BATS_TESTS_DIR}/templates/k8scontainterlimits_template.yaml
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl apply -f ${BATS_TESTS_DIR}/constraints/containers_must_be_limited.yaml"
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "constraint_enforced k8scontainerlimits container-must-have-limits"

  run kubectl apply -f ${BATS_TESTS_DIR}/bad/opa_no_limits.yaml
  assert_match 'denied the request' "${output}"
  assert_failure

  kubectl apply -f ${BATS_TESTS_DIR}/good/opa.yaml
}

@test "deployment test" {
  kubectl apply -f ${BATS_TESTS_DIR}/bad/bad_deployment.yaml
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl get deploy -n gatekeeper-test-playground opa-test-deployment -o yaml | grep unavailableReplicas"
}

@test "waiting for namespaces to be synced using metrics endpoint" {
  kubectl run temp --image=curlimages/curl -- tail -f /dev/null
  kubectl wait --for=condition=Ready --timeout=60s pod temp

  num_namespaces=$(kubectl get ns -o json | jq '.items | length')
  local pod_ip="$(kubectl -n ${GATEKEEPER_NAMESPACE} get pod -l gatekeeper.sh/operation=webhook -ojson | jq --raw-output '[.items[].status.podIP][0]' | sed 's#\.#-#g')"
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl exec -it temp -- curl http://${pod_ip}.${GATEKEEPER_NAMESPACE}.pod:8888/metrics | grep 'gatekeeper_sync{kind=\"Namespace\",status=\"active\"} ${num_namespaces}'"
  kubectl delete pod temp
}

@test "unique labels test" {
  kubectl apply -f ${BATS_TESTS_DIR}/templates/k8suniquelabel_template.yaml
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl apply -f ${BATS_TESTS_DIR}/constraints/all_cm_gatekeeper_label_unique.yaml"
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "constraint_enforced k8suniquelabel cm-gk-label-unique"

  run kubectl apply -f ${BATS_TESTS_DIR}/bad/no_dupe_cm_2.yaml
  assert_match 'denied the request' "${output}"
  assert_failure
}

__required_labels_audit_test() {
  local expected="$1"
  local cstr="$(kubectl get k8srequiredlabels.constraints.gatekeeper.sh cm-must-have-gk-audit -ojson)"
  if [[ $? -ne 0 ]]; then
    echo "error retrieving constraint"
    return 1
  fi

  echo "${cstr}"

  local total_violations=$(echo "${cstr}" | jq '.status.totalViolations')
  if [[ "${total_violations}" -ne "${expected}" ]]; then
    echo "totalViolations is ${total_violations}, wanted ${expected}"
    return 2
  fi

  local audit_entries=$(echo "${cstr}" | jq '.status.violations | length')
  if [[ "${audit_entries}" -ne "${expected}" ]]; then
    echo "Audit entry count is ${audit_entries}, wanted ${expected}"
    return 3
  fi
}

@test "required labels audit test" {
  local expected=5
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "__required_labels_audit_test 5"
}

@test "emit events test" {
  # list events for easy debugging
  kubectl get events -n ${GATEKEEPER_NAMESPACE}
  events=$(kubectl get events -n ${GATEKEEPER_NAMESPACE} --field-selector reason=FailedAdmission -o json | jq -r '.items[] | select(.metadata.annotations.constraint_kind=="K8sRequiredLabels" )' | jq -s '. | length')
  [[ "$events" -ge 1 ]]

  events=$(kubectl get events -n ${GATEKEEPER_NAMESPACE} --field-selector reason=DryrunViolation -o json | jq -r '.items[] | select(.metadata.annotations.constraint_kind=="K8sRequiredLabels" )' | jq -s '. | length')
  [[ "$events" -ge 1 ]]

  events=$(kubectl get events -n ${GATEKEEPER_NAMESPACE} --field-selector reason=AuditViolation -o json | jq -r '.items[] | select(.metadata.annotations.constraint_kind=="K8sRequiredLabels" )' | jq -s '. | length')
  [[ "$events" -ge 1 ]]
}

__namespace_exclusion_test() {
  local exclusion_config="$1"
  local excluded_namespace="$2"

  # applying default sync config
  kubectl apply -n ${GATEKEEPER_NAMESPACE} -f ${BATS_TESTS_DIR}/sync.yaml

  kubectl apply -f ${BATS_TESTS_DIR}/constraints/all_cm_must_have_gatekeeper.yaml
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "constraint_enforced k8srequiredlabels cm-must-have-gk"

  run kubectl create configmap should-fail -n "${excluded_namespace}"
  assert_match 'denied the request' "${output}"
  assert_failure

  kubectl apply -n ${GATEKEEPER_NAMESPACE} -f ${BATS_TESTS_DIR}/${exclusion_config}
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl create configmap should-succeed -n ${excluded_namespace}"
}

@test "config namespace exclusion test (exact match)" {
  local exclusion_config="sync_with_exclusion_exact_match.yaml"
  local excluded_namespace="gatekeeper-excluded-namespace"
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "__namespace_exclusion_test ${exclusion_config} ${excluded_namespace}"
}

@test "config namespace exclusion test (prefix match)" {
  local exclusion_config="sync_with_exclusion_prefix_match.yaml"
  local excluded_namespace="gatekeeper-excluded-prefix-match-namespace"
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "__namespace_exclusion_test ${exclusion_config} ${excluded_namespace}"
}

@test "config namespace exclusion test (suffix match)" {
  local exclusion_config="sync_with_exclusion_suffix_match.yaml"
  local excluded_namespace="gatekeeper-excluded-suffix-match-namespace"
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "__namespace_exclusion_test ${exclusion_config} ${excluded_namespace}"
}

@test "disable http.send" {
  kubectl apply -f ${BATS_TESTS_DIR}/templates/use_http_send_template.yaml
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "constraint_enforced constrainttemplate k8sdenynamehttpsend"
  run kubectl apply -f ${BATS_TESTS_DIR}/bad/bad_http_send.yaml
  assert_failure
  run kubectl get constrainttemplate/k8sdenynamehttpsend -o jsonpath="{.status}"
  assert_match 'undefined function http.send' "${output}"
}

@test "external data provider crd is established" {
  if [ -z $ENABLE_EXTERNAL_DATA_TESTS ]; then
    skip "skipping external data tests"
  fi
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl wait --for condition=established --timeout=60s crd/providers.externaldata.gatekeeper.sh"
}

@test "gatekeeper external data validation and mutation test" {
  if [ -z $ENABLE_EXTERNAL_DATA_TESTS ]; then
    skip "skipping external data tests"
  fi

  if [ ! -f test/externaldata/dummy-provider/certs/ca.crt ]; then
    echo "Missing dummy-provider's CA cert. Please run test/externaldata/dummy-provider/scripts/generate-tls-certificate.sh to generate it."
    exit 1
  fi

  tmp=$(mktemp -d)

  # inject caBundle into the provider YAML
  cat <<EOF > ${tmp}/provider.yaml
$(cat test/externaldata/dummy-provider/manifest/provider.yaml)
  caBundle: $(cat test/externaldata/dummy-provider/certs/ca.crt | base64 | tr -d '\n')
EOF

  run kubectl apply -f ${tmp}/provider.yaml
  assert_success
  kubectl apply -f test/externaldata/dummy-provider/manifest/deployment.yaml
  assert_success
  kubectl apply -f test/externaldata/dummy-provider/manifest/service.yaml
  assert_success
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl wait --for=condition=Ready --timeout=60s pod -l run=dummy-provider -n gatekeeper-system"

  # validation test
  kubectl apply -f test/externaldata/dummy-provider/policy/template.yaml
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl apply -f test/externaldata/dummy-provider/policy/constraint.yaml"
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "constraint_enforced k8sexternaldata dummy"

  run kubectl apply -f test/externaldata/dummy-provider/policy/examples/error.yaml
  assert_match 'denied the request' "${output}"
  assert_match 'error_test/image:latest_invalid' "${output}"
  assert_failure

  run kubectl apply -f test/externaldata/dummy-provider/policy/examples/system-error.yaml
  assert_match 'denied the request' "${output}"
  assert_match 'testing system error' "${output}"
  assert_failure

  run kubectl apply -f test/externaldata/dummy-provider/policy/examples/valid.yaml
  assert_success

  # mutation test
  run kubectl apply -f test/externaldata/dummy-provider/mutation/valid.yaml
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "mutator_enforced AssignMetadata annotate-owner"
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "mutator_enforced Assign a-sidecar-injection"
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "mutator_enforced Assign b-assign-image"

  run kubectl run nginx --image=nginx --dry-run=server --output json
  assert_success
  assert_match "kubernetes-admin_valid" "$(jq -r '.metadata.annotations["external-data-username"]' <<< ${output})"
  assert_match "nginx_valid" "$(jq -r '.spec.containers[0].image' <<< ${output})"
  assert_match "busybox_valid" "$(jq -r '.spec.containers[1].image' <<< ${output})"

  run kubectl apply -f test/externaldata/dummy-provider/mutation/invalid_assignmetadata.yaml
  assert_match 'only username data source is supported' "${output}"
  assert_match 'invalid location' "${output}"
  assert_failure

  run kubectl apply -f test/externaldata/dummy-provider/mutation/invalid_assign.yaml
  assert_match '`default` must not be empty when `failurePolicy` is set to `UseDefault`' "${output}"
  assert_match 'cannot assign external data response to a list' "${output}"
  assert_failure

  # simulate key error
  run kubectl run busybox --image=error_busybox --dry-run=server --output json
  assert_match 'error_busybox_invalid' "${output}"
  assert_failure

  # simulate system error
  run kubectl run busybox --image=busybox:latest_systemError --dry-run=server --output json
  assert_match 'testing system error' "${output}"
  assert_failure

  # schema conflict test
  run kubectl apply -f test/externaldata/dummy-provider/mutation/schema_conflict.yaml
  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "mutator_enforced Assign schema-conflict"

  wait_for_process ${WAIT_TIME} ${SLEEP_TIME} "kubectl get assign schema-conflict -ojson | jq -r -e '.status.byPod[0].errors[0]'"
  run kubectl get assign schema-conflict -o jsonpath="{.status}"
  assert_match 'Assign.mutations.gatekeeper.sh /b-assign-image,Assign.mutations.gatekeeper.sh /schema-conflict' "${output}"
  assert_match 'ErrConflictingSchema' "${output}"

  kubectl delete --ignore-not-found -f test/externaldata/dummy-provider/manifest
  kubectl delete --ignore-not-found -f test/externaldata/dummy-provider/mutation
  kubectl delete --ignore-not-found deploy error-deployment valid-deployment system-error-deployment
  kubectl delete --ignore-not-found constrainttemplate k8sexternaldata
}
