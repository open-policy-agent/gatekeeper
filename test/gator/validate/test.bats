#!/usr/bin/env bats

####################################################################################################
# HELPER FUNCTIONS
####################################################################################################

# match_substring checks that got (arg1) contains the substring want (arg2).
# If a match is not found, got will be printed and the program will exit with
# status code 1.
match_substring () {
  got="${1}"
  want="${2}"

  if ! [[ "$got" =~ .*"$want".* ]]; then
    printf "ERROR: expected output to contain substring '%s'\n" "$want"
    printf "GOT: %s\n" "$got"
    exit 1
  fi
}

# match_yaml_msg checks that the gator validate full yaml output (arg1)
# contains the `msg: ` field and then matches that `msg` field against the
# "want" message (arg2).  If either of these checks fail, helpful errors will
# be printed and the program will exit 1.
match_yaml_msg () {
  yaml_output="${1}"
  want_msg="${2}"

  if ! got=$(echo -n "$yaml_output" | yq eval '.[0].msg' - --exit-status); then
    printf "ERROR: failed to evaluate output\n"
    printf "GOT: %s\n" "$yaml_output"
    exit 1
  fi

  if  [ "$got" != "$want_msg" ]; then
    printf "ERROR: expected violation message '%s'\n" "$want_msg"
    printf "GOT: %s\n" "$yaml_output"
    exit 1
  fi
}

####################################################################################################
# END OF HELPER FUNCTIONS
####################################################################################################

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
  bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/policies/default" --filename="$BATS_TEST_DIRNAME/fixtures/manifests/no-policies/no-violations.yaml"
  if [ "$?" -ne 0 ]; then
    printf "ERROR: got exit status %s but wanted 0\n" "$?"
    exit 1
  fi
}

@test "correctly finds violations from objects in a filesystem" {
  ! bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/policies/default" --filename="$BATS_TEST_DIRNAME/fixtures/manifests/no-policies/with-violations.yaml"
}

@test "expects user to input data" {
  run bin/gator validate
  [ "$status" -eq 1 ]
  err_substring="no input data"
  match_substring "${output[*]}" "${err_substring}"
}

@test "disallows invalid template" {
  run bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/manifests/invalid-resources/template.yaml"
  [ "$status" -eq 1 ]
  err_substring="reading yaml source"
  match_substring "${output[*]}" "${err_substring}"
}

@test "disallows invalid constraint" {
  run bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/manifests/invalid-resources/constraint.yaml"
  [ "$status" -eq 1 ]
  err_substring="reading yaml source"
  match_substring "${output[*]}" "${err_substring}"
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
  # Confirm we still get our violation output
  want_msg="Container <tomcat> in your <Pod> <test-pod1> has no <readinessProbe>" 
  match_yaml_msg "${output[*]}" "${want_msg}"
}

@test "correctly ingests files of different extensions, skipping bad extensions, and producing correct violations" {
  run bin/gator validate --filename="$BATS_TEST_DIRNAME/fixtures/manifests/different-extensions" -o=yaml
  [ "$status" -eq 1 ]

  # Confirm we still get our violation output
  want_msg="Container <tomcat> in your <Pod> <test-pod1> has no <readinessProbe>" 
  match_yaml_msg "${output[*]}" "${want_msg}"
}

@test "enforcementAction=deny causes 1 exit status and violations output" {
  run bin/gator validate \
    -f="$BATS_TEST_DIRNAME/fixtures/policies/default/template_k8srequiredprobes.yaml" \
    -f="$BATS_TEST_DIRNAME/fixtures/policies/enforcement_action/k8srequiredprobes/deny.yaml" \
    -f="$BATS_TEST_DIRNAME/fixtures/manifests/no-policies/with-violations.yaml" \
    -o=yaml

  [ "$status" -eq 1 ]

  # Confirm we still get our violation output
  want_msg="Container <tomcat> in your <Pod> <test-pod1> has no <readinessProbe>" 
  match_yaml_msg "${output[*]}" "${want_msg}"
}

@test "enforcementAction=warn causes 0 exit status and violations output" {
  run bin/gator validate -f="$BATS_TEST_DIRNAME/fixtures/policies/default/template_k8srequiredprobes.yaml" \
    -f="$BATS_TEST_DIRNAME/fixtures/policies/enforcement_action/k8srequiredprobes/warn.yaml" \
    -f="$BATS_TEST_DIRNAME/fixtures/manifests/no-policies/with-violations.yaml" \
    -o=yaml

  [ "$status" -eq 0 ]

  # Confirm we still get our violation output
  want_msg="Container <tomcat> in your <Pod> <test-pod1> has no <readinessProbe>" 
  match_yaml_msg "${output[*]}" "${want_msg}"
}

@test "enforcementAction=dryrun causes 0 exit status and violations output" {
  run bin/gator validate \
    -f="$BATS_TEST_DIRNAME/fixtures/policies/default/template_k8srequiredprobes.yaml" \
    -f="$BATS_TEST_DIRNAME/fixtures/policies/enforcement_action/k8srequiredprobes/dryrun.yaml" \
    -f="$BATS_TEST_DIRNAME/fixtures/manifests/no-policies/with-violations.yaml" \
    -o=yaml

  [ "$status" -eq 0 ]

  # Confirm we still get our violation output
  want_msg="Container <tomcat> in your <Pod> <test-pod1> has no <readinessProbe>" 
  match_yaml_msg "${output[*]}" "${want_msg}"
}

@test "enforcementAction=[anything else] causes 0 exit status and violations output" {
  run bin/gator validate \
    -f="$BATS_TEST_DIRNAME/fixtures/policies/default/template_k8srequiredprobes.yaml" \
    -f="$BATS_TEST_DIRNAME/fixtures/policies/enforcement_action/k8srequiredprobes/foo.yaml" \
    -f="$BATS_TEST_DIRNAME/fixtures/manifests/no-policies/with-violations.yaml" \
    -o=yaml

  [ "$status" -eq 0 ]

  # Confirm we still get our violation output
  want_msg="Container <tomcat> in your <Pod> <test-pod1> has no <readinessProbe>" 
  match_yaml_msg "${output[*]}" "${want_msg}"
}
