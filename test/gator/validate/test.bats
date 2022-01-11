#!/usr/bin/env bats

@test "manifest with no violations piped to stdin returns 0 exit status" {
  bin/gator validate < "$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/no-violations.yaml"
  if [ "$?" -ne 0 ]; then
    printf "ERROR: got exit status %s but wanted 0\n" "$?"
    exit 1
  fi
}

@test "manifest with violations piped to stdin returns 1 exit status" {
  ! bin/gator validate < "$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/with-violations.yaml"
}

@test "manifest with no violations included as flag returns 0 exit status" {
  bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/no-violations.yaml"
  if [ "$?" -ne 0 ]; then
    printf "ERROR: got exit status %s but wanted 0\n" "$?"
    exit 1
  fi
}

@test "manifest with violations included as flag returns 1 exit status" {
  ! bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/with-violations.yaml"
}

@test "multiple files passed in flags is supported" {
  bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/no-violations.yaml" --filename="$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/no-violations-2.yaml"
  if [ "$?" -ne 0 ]; then
    printf "ERROR: got exit status %s but wanted 0\n" "$?"
    exit 1
  fi
}

@test "stdin and flag are not supported in combination" {
  ! bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/no-violations.yaml" < "$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/no-violations-2.yaml"
}

@test "correctly returns no violations from objects in a filesystem" {
  bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/policies" --filename="$BATS_TEST_DIRNAME/fixtures/manifests/no-policies/no-violations.yaml"
  if [ "$?" -ne 0 ]; then
    printf "ERROR: got exit status %s but wanted 0\n" "$?"
    exit 1
  fi
}

@test "correctly finds violations from objects in a filesystem" {
  ! bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/policies" --filename="$BATS_TEST_DIRNAME/fixtures/manifests/no-policies/with-violations.yaml"
}

@test "expects user to input data" {
  run bin/gator validate
  [ "$status" -eq 1 ]
  err_substring="no input data"
  if ! [[ "${output[*]}" =~ .*"$err_substring".* ]]; then
    printf "ERROR: expected output to contain substring '%s'\n" "$err_substring"
    printf "OUTPUT: %s\n" "${output[*]}"
    exit 1
  fi
}

@test "disallows invalid template" {
  run bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/manifests/invalid-resources/template.yaml"
  [ "$status" -eq 1 ]
  err_substring="reading yaml source"
  if ! [[ "${output[*]}" =~ .*"$err_substring".* ]]; then
    printf "ERROR: expected output to contain substring '%s'\n" "$err_substring"
    printf "OUTPUT: %s\n" "${output[*]}"
    exit 1
  fi
}

@test "disallows invalid constraint" {
  run bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/manifests/invalid-resources/constraint.yaml"
  [ "$status" -eq 1 ]
  err_substring="reading yaml source"
  if ! [[ "${output[*]}" =~ .*"$err_substring".* ]]; then
    printf "ERROR: expected output to contain substring '%s'\n" "$err_substring"
    printf "OUTPUT: %s\n" "${output[*]}"
    exit 1
  fi
}

@test "outputs valid json when flag is specified" {
  run bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/with-violations.yaml" --output=json
  [ "$status" -eq 1 ]
  # uses jq version `jq-1.6`
  if ! (echo -n "${output[*]}" | jq); then
    printf "ERROR: expected output to be valid json\n"
    printf "OUTPUT: %s\n" "${output[*]}"
    exit 1
  fi
}

@test "outputs valid yaml when flag is specified" {
  run bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/with-violations.yaml" -o=yaml
  [ "$status" -eq 1 ]
  # yq (https://github.com/mikefarah/yq/) version 4.16.2
  # TODO (juliankatz): Turn this section into a function and use it to improve checks in other test cases
  if ! (echo -n "${output[*]}" | yq eval '.[0].msg' - --exit-status); then
    printf "ERROR: expected output to be valid yaml\n"
    printf "OUTPUT: %s\n" "${output[*]}"
    exit 1
  fi
}

@test "correctly ingests files of different extensions, skipping bad extensions, and producing correct violations" {
  run bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/manifests/different-extensions" -o=yaml
  [ "$status" -eq 1 ]

  got=$(echo -n "${output[*]}" | yq eval '.[0].msg' - --exit-status)
  want="Container <tomcat> in your <Pod> <test-pod1> has no <readinessProbe>" 

  if  [ "$got" != "$want" ]; then
    printf "ERROR: expected violation message '%s'\n" "$want"
    printf "OUTPUT: %s\n" "${output[*]}"
    exit 1
  fi
}
