#!/usr/bin/env bats

load helpers

BATS_TESTS_DIR=test/bats/tests
WAIT_TIME=120
SLEEP_TIME=1

@test "gatekeeper-controller-manager is running" {
  run wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl -n gatekeeper-system wait --for=condition=Ready --timeout=60s pod -l control-plane=controller-manager"
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

@test "required labels test" {
  run kubectl apply -f ${BATS_TESTS_DIR}/templates/k8srequiredlabels_template.yaml
  assert_success

	wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl -n gatekeeper-system wait --for condition=established --timeout=60s crd/k8srequiredlabels.constraints.gatekeeper.sh"

  run kubectl apply -f ${BATS_TESTS_DIR}/constraints/all_ns_must_have_gatekeeper.yaml
  assert_success

  wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl get k8srequiredlabels.constraints.gatekeeper.sh ns-must-have-gk -o yaml | grep enforced"

  run kubectl apply -f ${BATS_TESTS_DIR}/good/good_ns.yaml
  assert_success

  run kubectl apply -f ${BATS_TESTS_DIR}/bad/bad_ns.yaml
  assert_match 'denied the request' "$output"
  assert_failure
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

  wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl get k8scontainerlimits.constraints.gatekeeper.sh container-must-have-limits -o yaml | grep enforced"

  run kubectl apply -f ${BATS_TESTS_DIR}/bad/opa_no_limits.yaml
  assert_match 'denied the request' "$output"
  assert_failure

  run kubectl apply -f ${BATS_TESTS_DIR}/good/opa.yaml
  assert_success
}

@test "waiting for namespaces to be synced" {
	wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl get ns no-dupes -o jsonpath='{.metadata.finalizers}' | grep finalizers.gatekeeper.sh/sync"
}

@test "unique labels test" {
  run kubectl apply -f ${BATS_TESTS_DIR}/templates/k8suniquelabel_template.yaml
  assert_success

	wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl -n gatekeeper-system wait --for condition=established --timeout=60s crd/k8suniquelabel.constraints.gatekeeper.sh"

  run kubectl apply -f ${BATS_TESTS_DIR}/constraints/all_ns_gatekeeper_label_unique.yaml
  assert_match 'k8suniquelabel.constraints.gatekeeper.sh/ns-gk-label-unique created' "$output"
  assert_success

  wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl get k8suniquelabel.constraints.gatekeeper.sh ns-gk-label-unique -o yaml | grep enforced"

  run kubectl apply -f ${BATS_TESTS_DIR}/bad/no_dupe_ns_2.yaml
  assert_match 'denied the request' "$output"
  assert_failure
}

@test "required labels audit test" {
  wait_for_process $WAIT_TIME $SLEEP_TIME "kubectl get k8srequiredlabels.constraints.gatekeeper.sh ns-must-have-gk -o json | jq '.status.violations[]'"

  violations=$(kubectl get k8srequiredlabels.constraints.gatekeeper.sh ns-must-have-gk -o json | jq '.status.violations | length')
  [[ "$violations" -eq 5 ]]

  totalViolations=$(kubectl get k8srequiredlabels.constraints.gatekeeper.sh ns-must-have-gk -o json | jq '.status.totalViolations')
  [[ "$totalViolations" -eq 5 ]]
}
