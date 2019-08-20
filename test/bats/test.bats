#!/usr/bin/env bats

load helpers

BATS_TESTS_DIR=test/bats/tests

@test "gatekeeper-controller-manager running" {
  run kubectl -n gatekeeper-system wait --for=condition=Ready --timeout=60s pod/gatekeeper-controller-manager-0
  assert_success
}

@test "constrainttemplates crd is established" {
  run kubectl -n gatekeeper-system wait --for condition=established --timeout=60s crd/constrainttemplates.templates.gatekeeper.sh
  assert_success
}

@test "apply config" {
  run kubectl apply -f ${BATS_TESTS_DIR}/sync.yaml
  assert_success
}

@test "required labels test" {
  run kubectl apply -f ${BATS_TESTS_DIR}/templates/k8srequiredlabels_template.yaml
  assert_success

  while [[ -z $(kubectl -n gatekeeper-system wait --for condition=established --timeout=60s crd/k8srequiredlabels.constraints.gatekeeper.sh) ]]; do echo "waiting for crd" && sleep 1; done

  run kubectl apply -f ${BATS_TESTS_DIR}/constraints/all_ns_must_have_gatekeeper.yaml
  assert_success

  while [[ $(kubectl get k8srequiredlabels.constraints.gatekeeper.sh ns-must-have-gk -o json | jq '.status.byPod[].enforced') != "true" ]]; do echo "waiting for constraint" && sleep 1; done

  run echo $(kubectl get k8srequiredlabels.constraints.gatekeeper.sh ns-must-have-gk -o json | jq -r '.status.byPod[].enforced')
  assert_equal 'true' "$output"
  assert_success

  sleep 60

  run kubectl apply -f ${BATS_TESTS_DIR}/good/good_ns.yaml
  assert_success

  run kubectl apply -f ${BATS_TESTS_DIR}/bad/bad_ns.yaml
  assert_match 'denied the request' "$output"
  assert_failure

  result=$(kubectl get k8srequiredlabels.constraints.gatekeeper.sh ns-must-have-gk -o json | jq '.status.violations[] | length' | head -1)
  [[ "$result" -eq 4 ]]
}

@test "unique labels test" {
  run kubectl apply -f ${BATS_TESTS_DIR}/templates/k8suniquelabel_template.yaml
  assert_success

  while [[ -z $(kubectl -n gatekeeper-system wait --for condition=established --timeout=60s crd/k8suniquelabel.constraints.gatekeeper.sh) ]]; do echo "waiting for crd" && sleep 1; done

  run kubectl apply -f ${BATS_TESTS_DIR}/constraints/all_ns_gatekeeper_label_unique.yaml
  assert_match 'k8suniquelabel.constraints.gatekeeper.sh/ns-gk-label-unique created' "$output"
  assert_success

  while [[ $(kubectl get k8suniquelabel.constraints.gatekeeper.sh ns-gk-label-unique -o json | jq '.status.byPod[].enforced') != "true" ]]; do echo "waiting for constraint" && sleep 1; done

  run echo $(kubectl get k8suniquelabel.constraints.gatekeeper.sh ns-gk-label-unique -o json | jq -r '.status.byPod[].enforced')
  assert_equal 'true' "$output"
  assert_success

  run kubectl apply -f ${BATS_TESTS_DIR}/good/no_dupe_ns.yaml
  assert_success

  run kubectl apply -f ${BATS_TESTS_DIR}/bad/no_dupe_ns_2.yaml
  assert_match 'denied the request' "$output"
  assert_failure
}
