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

get_ca_cert() {
  destination="$1"
  if [ $(kubectl get secret -n gatekeeper-system gatekeeper-webhook-server-cert -o jsonpath='{.data.ca\.crt}' | wc -w) -eq 0 ]; then
    return 1
  fi
  kubectl get secret -n gatekeeper-system gatekeeper-webhook-server-cert -o jsonpath='{.data.ca\.crt}' | base64 -d >$destination
}

constraint_enforced() {
  local kind="$1"
  local name="$2"
  local pod_list="$(kubectl -n gatekeeper-system get pod -l gatekeeper.sh/operation=webhook -o json)"
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
