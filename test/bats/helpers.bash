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

compare_generation() {
  kind="$1"
  constraint="$2"

  [[ "$(kubectl get ${kind}.constraints.gatekeeper.sh ${constraint} -o json | jq '.status.byPod[0].observedGeneration')" = "$(kubectl get ${kind}.constraints.gatekeeper.sh ${constraint} -o json | jq '.metadata.generation')" ]]
}

compare_count() {
  kind="$1"
  constraint="$2"
  podcount="$3"

  [[ "$(kubectl get ${kind}.constraints.gatekeeper.sh ${constraint} -o yaml | grep -c 'id: gatekeeper-controller-manager')" = $podcount ]]
}
