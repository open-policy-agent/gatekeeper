#!/usr/bin/env bats

load helpers

BATS_TESTS_DIR=test/bats/tests
WAIT_TIME=120
SLEEP_TIME=1
CLEAN_CMD="echo cleaning..."

teardown() {
  bash -c "${CLEAN_CMD}"
}

@test "gatekeeper-controller-manager is running" {
  run wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl -n gatekeeper-system wait --for=condition=Ready --timeout=60s pod -l control-plane=controller-manager"
  assert_success
}

@test "gatekeeper-audit is running" {
  run wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl -n gatekeeper-system wait --for=condition=Ready --timeout=60s pod -l control-plane=audit-controller"
  assert_success
}

@test "namespace label webhook is serving" {
  cert=$(mktemp)
  CLEAN_CMD="${CLEAN_CMD}; rm ${CERT}"
  wait_for_process $WAIT_TIME $SLEEP_TIME "get_ca_cert ${cert}"

  kubectl port-forward -n gatekeeper-system deployment/gatekeeper-controller-manager 8443:8443 &
  FORWARDING_PID=$!
  CLEAN_CMD="${CLEAN_CMD}; kill ${FORWARDING_PID}"

  run wait_for_process $WAIT_TIME $SLEEP_TIME "curl -f -v --resolve gatekeeper-webhook-service.gatekeeper-system.svc:8443:127.0.0.1 --cacert ${cert} https://gatekeeper-webhook-service.gatekeeper-system.svc:8443/v1/admitlabel"
  assert_success
}

@test "constrainttemplates crd is established" {
  wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl -n gatekeeper-system wait --for condition=established --timeout=60s crd/constrainttemplates.templates.gatekeeper.sh"

  run kubectl -n gatekeeper-system get crd/constrainttemplates.templates.gatekeeper.sh
  assert_success
}

@test "waiting for validating webhook" {
  wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl get validatingwebhookconfigurations.admissionregistration.k8s.io gatekeeper-validating-webhook-configuration"

  run kubectl get validatingwebhookconfigurations.admissionregistration.k8s.io gatekeeper-validating-webhook-configuration
  assert_success
}

@test "applying sync config" {
  run kubectl apply -f ${BATS_TESTS_DIR}/sync.yaml
  assert_success
}

# creating a namespace early so it will have time to sync
@test "create namespace for unique labels test" {
  run kubectl apply -f ${BATS_TESTS_DIR}/good/no_dupe_ns.yaml
  assert_success
}

@test "no ignore label unless namespace is exempt test" {
  run kubectl apply -f ${BATS_TESTS_DIR}/good/ignore_label_ns.yaml
  assert_failure
}

@test "gatekeeper-system ignore label can be patched" {
  run kubectl patch ns gatekeeper-system --type=json -p='[{"op": "replace", "path": "/metadata/labels/admission.gatekeeper.sh~1ignore", "value": "ignore-label-test-passed"}]'
  assert_success
}

@test "required labels dryrun test" {
  run kubectl apply -f ${BATS_TESTS_DIR}/templates/k8srequiredlabels_template.yaml
  assert_success

  wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl -n gatekeeper-system wait --for condition=established --timeout=60s crd/k8srequiredlabels.constraints.gatekeeper.sh"

  run kubectl apply -f ${BATS_TESTS_DIR}/constraints/all_ns_must_have_gatekeeper.yaml
  assert_success

  wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl get k8srequiredlabels.constraints.gatekeeper.sh ns-must-have-gk -o yaml | grep 'id: gatekeeper-controller-manager'"

  run kubectl apply -f ${BATS_TESTS_DIR}/good/good_ns.yaml
  assert_success

  run kubectl apply -f ${BATS_TESTS_DIR}/bad/bad_ns.yaml
  assert_match 'denied the request' "$output"
  assert_failure

  run kubectl apply -f ${BATS_TESTS_DIR}/constraints/all_ns_must_have_gatekeeper-dryrun.yaml
  assert_success

  run wait_for_process $WAIT_TIME $SLEEP_TIME "compare_generation k8srequiredlabels ns-must-have-gk"
  assert_success

  wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl get k8srequiredlabels.constraints.gatekeeper.sh ns-must-have-gk -o json | jq '.spec.enforcementAction' | grep dryrun"

  # deploying a violation with dryrun enforcement action will be accepted
  run kubectl apply -f ${BATS_TESTS_DIR}/bad/bad_ns.yaml
  assert_success

  CLEAN_CMD="kubectl delete -f ${BATS_TESTS_DIR}/bad/bad_ns.yaml"
}

@test "container limits test" {
  run kubectl apply -f ${BATS_TESTS_DIR}/templates/k8scontainterlimits_template.yaml
  assert_success

  wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl -n gatekeeper-system wait --for condition=established --timeout=60s crd/k8scontainerlimits.constraints.gatekeeper.sh"

  run kubectl get crd/k8scontainerlimits.constraints.gatekeeper.sh
  assert_success

  run kubectl apply -f ${BATS_TESTS_DIR}/constraints/containers_must_be_limited.yaml
  assert_match 'k8scontainerlimits.constraints.gatekeeper.sh/container-must-have-limits created' "$output"
  assert_success

  wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl get k8scontainerlimits.constraints.gatekeeper.sh container-must-have-limits -o yaml | grep 'id: gatekeeper-controller-manager'"

  run kubectl apply -f ${BATS_TESTS_DIR}/bad/opa_no_limits.yaml -n good-ns
  assert_match 'denied the request' "$output"
  assert_failure

  run kubectl apply -f ${BATS_TESTS_DIR}/good/opa.yaml
  assert_success
}

@test "deployment test" {
  run kubectl apply -f ${BATS_TESTS_DIR}/bad/bad_deployment.yaml
  assert_success

  wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl get deploy opa-test-deployment -o yaml | grep unavailableReplicas"
}

@test "waiting for namespaces to be synced using metrics endpoint" {
  kubectl port-forward -n gatekeeper-system deployment/gatekeeper-controller-manager 8888:8888 &
  FORWARDING_PID=$!
  CLEAN_CMD="kill ${FORWARDING_PID}"

  num_namespaces=$(kubectl get ns -o json | jq '.items | length')
  run wait_for_process $WAIT_TIME $SLEEP_TIME "curl -s 127.0.0.1:8888/metrics | grep 'gatekeeper_sync{kind=\"Namespace\",status=\"active\"} ${num_namespaces}'"
  assert_success
}

@test "unique labels test" {
  run kubectl apply -f ${BATS_TESTS_DIR}/templates/k8suniquelabel_template.yaml
  assert_success

  wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl -n gatekeeper-system wait --for condition=established --timeout=60s crd/k8suniquelabel.constraints.gatekeeper.sh"

  run kubectl apply -f ${BATS_TESTS_DIR}/constraints/all_ns_gatekeeper_label_unique.yaml
  assert_match 'k8suniquelabel.constraints.gatekeeper.sh/ns-gk-label-unique created' "$output"
  assert_success

  wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl get k8suniquelabel.constraints.gatekeeper.sh ns-gk-label-unique -o yaml | grep 'id: gatekeeper-controller-manager'"

  run kubectl apply -f ${BATS_TESTS_DIR}/bad/no_dupe_ns_2.yaml
  assert_match 'denied the request' "$output"
  assert_failure
}

@test "required labels audit test" {
  wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl get k8srequiredlabels.constraints.gatekeeper.sh ns-must-have-gk -o json | jq '.status.violations[]'"

  violations=$(kubectl get k8srequiredlabels.constraints.gatekeeper.sh ns-must-have-gk -o json | jq '.status.violations | length')
  [[ "$violations" -eq 6 ]]

  totalViolations=$(kubectl get k8srequiredlabels.constraints.gatekeeper.sh ns-must-have-gk -o json | jq '.status.totalViolations')
  [[ "$totalViolations" -eq 6 ]]
}

@test "config namespace exclusion test" {
  run kubectl create ns excluded-namespace
  assert_success

  run kubectl apply -f ${BATS_TESTS_DIR}/bad/opa_no_limits.yaml -n excluded-namespace
  assert_success
}
