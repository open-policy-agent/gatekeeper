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

# match_yaml_msg checks that the gator test full yaml output (arg1)
# contains the `msg: ` field and then matches that `msg` field against the
# "want" message (arg2).  Multiple error messages can be checked by passing in
# a violation index (arg3).  arg3 defaults to `0` for use when there is a
# single violation. If either of these checks fail, helpful errors will be
# printed and the program will exit 1.
match_yaml_msg () {
  yaml_output="${1}"
  want_msg="${2}"
  violation_index="${3:-0}"

  if ! got=$(echo -n "$yaml_output" | yq eval ".[${violation_index}].result.msg" - --exit-status); then
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

@test "gator test doesn't wait on stdin input" {
  # this should fail with "no input data identified"
  ! bin/gator test
}

@test "manifest with no violations piped to stdin returns 0 exit status" {
  bin/gator test < "$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/no-violations.yaml"
  if [ "$?" -ne 0 ]; then
    printf "ERROR: got exit status %s but wanted 0\n" "$?"
    exit 1
  fi
}

@test "manifest with violations redirected to stdin returns 1 exit status" {
  ! bin/gator test < "$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/with-violations.yaml"
}

@test "manifest with violations piped to stdin returns 1 exit status" {
  # first test that we fail the command
  ! cat "$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/with-violations.yaml" | bin/gator test

  output=$(! cat "$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/with-violations.yaml" | bin/gator test)

  # now test that the failure reason is a violation
  match_substring "${output[*]}" "Container <tomcat> in your <Pod> <test-pod1> has no <readinessProbe>"
}

@test "manifest with no violations included as flag returns 0 exit status" {
  bin/gator test --filename="$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/no-violations.yaml"
  if [ "$?" -ne 0 ]; then
    printf "ERROR: got exit status %s but wanted 0\n" "$?"
    exit 1
  fi
}

@test "manifest with violations included as flag returns 1 exit status" {
  ! bin/gator test --filename="$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/with-violations.yaml"
}

@test "multiple files passed in flags is supported" {
 run bin/gator test --filename="$BATS_TEST_DIRNAME/fixtures/manifests/no-policies/with-violations.yaml" --filename="$BATS_TEST_DIRNAME/fixtures/policies/default" -oyaml
  [ "$status" -eq 1 ]
  match_yaml_msg "$output" "Container <tomcat> in your <Pod> <test-pod1> has no <readinessProbe>"
}

@test "reports error if provided file is not a directory and has disallowed extension" {
  tmp_manifest=$(mktemp) 
  cp "$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/with-violations.yaml" "$tmp_manifest"

  run bin/gator test --filename="$tmp_manifest" -o=yaml
  [ "$status" -eq 1 ]
  err_substring="must be of extensions: [.yaml .yml .json]"
  match_substring "${output[*]}" "${err_substring}"
}

@test "stdin and flag are supported in combination" {
  output=$(! bin/gator test --filename="$BATS_TEST_DIRNAME/fixtures/policies/default" -o=yaml < "$BATS_TEST_DIRNAME/fixtures/manifests/no-policies/with-violations.yaml")
  # since the `run` command doesn't support redirects, it's impractical to
  # confirm the `1` exit code.  We'll instead just confirm the violation is
  # working, and rely on other tests to confirm that `1` is being returned when
  # violations are found.
  match_yaml_msg "$output" "Container <tomcat> in your <Pod> <test-pod1> has no <readinessProbe>"
}

@test "correctly returns no violations from objects in a filesystem" {
  bin/gator test --filename="$BATS_TEST_DIRNAME/fixtures/policies/default" --filename="$BATS_TEST_DIRNAME/fixtures/manifests/no-policies/no-violations.yaml"
  if [ "$?" -ne 0 ]; then
    printf "ERROR: got exit status %s but wanted 0\n" "$?"
    exit 1
  fi
}

@test "correctly finds violations from objects in a filesystem" {
  ! bin/gator test --filename="$BATS_TEST_DIRNAME/fixtures/policies/default" --filename="$BATS_TEST_DIRNAME/fixtures/manifests/no-policies/with-violations.yaml"
}

@test "expects user to input data" {
  run bin/gator test
  [ "$status" -eq 1 ]
  err_substring="no input data identified"
  match_substring "${output[*]}" "${err_substring}"
}

@test "disallows invalid template" {
  run bin/gator test --filename="$BATS_TEST_DIRNAME/fixtures/manifests/invalid-resources/template.yaml"
  [ "$status" -eq 1 ]
  err_substring="reading yaml source"
  match_substring "${output[*]}" "${err_substring}"
}

@test "disallows invalid constraint" {
  run bin/gator test --filename="$BATS_TEST_DIRNAME/fixtures/manifests/invalid-resources/constraint.yaml"
  [ "$status" -eq 1 ]
  err_substring="reading yaml source"
  match_substring "${output[*]}" "${err_substring}"
}

@test "outputs valid json when flag is specified" {
  run bin/gator test --filename="$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/with-violations.yaml" --output=json
  [ "$status" -eq 1 ]
  # uses jq version `jq-1.6`
  if ! (echo -n "${output[*]}" | jq); then
    printf "ERROR: expected output to be valid json\n"
    printf "OUTPUT: %s\n" "${output[*]}"
    exit 1
  fi
}

@test "outputs valid yaml when flag is specified" {
  run bin/gator test --filename="$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/with-violations.yaml" -o=yaml
  [ "$status" -eq 1 ]
  # Confirm we still get our violation output
  want_msg="Container <tomcat> in your <Pod> <test-pod1> has no <readinessProbe>" 
  match_yaml_msg "${output[*]}" "${want_msg}"
}

@test "correctly ingests files of different extensions, skipping bad extensions, and producing correct violations" {
  run bin/gator test --filename="$BATS_TEST_DIRNAME/fixtures/manifests/different-extensions" -o=yaml
  [ "$status" -eq 1 ]

  # Confirm we still get our violation output
  want_msg="Container <tomcat> in your <Pod> <test-pod1> has no <readinessProbe>" 
  match_yaml_msg "${output[*]}" "${want_msg}"
}

@test "enforcementAction=deny causes 1 exit status and violations output" {
  run bin/gator test \
    -f="$BATS_TEST_DIRNAME/fixtures/policies/default/template_k8srequiredprobes.yaml" \
    -f="$BATS_TEST_DIRNAME/fixtures/policies/enforcement_action/k8srequiredprobes/deny.yaml" \
    -f="$BATS_TEST_DIRNAME/fixtures/manifests/no-policies/with-violations.yaml" \
    -o=yaml

  [ "$status" -eq 1 ]

  # Confirm we still get our violation output
  want_msg="Container <tomcat> in your <Pod> <test-pod1> has no <readinessProbe>" 
  match_yaml_msg "${output[*]}" "${want_msg}"
}

@test "enforcementAction=[anything else] causes 0 exit status and violations output" {
  run bin/gator test \
    -f="$BATS_TEST_DIRNAME/fixtures/policies/default/template_k8srequiredprobes.yaml" \
    -f="$BATS_TEST_DIRNAME/fixtures/policies/enforcement_action/k8srequiredprobes/foo.yaml" \
    -f="$BATS_TEST_DIRNAME/fixtures/manifests/no-policies/with-violations.yaml" \
    -o=yaml

  [ "$status" -eq 0 ]

  # Confirm we still get our violation output
  want_msg="Container <tomcat> in your <Pod> <test-pod1> has no <readinessProbe>" 
  match_yaml_msg "${output[*]}" "${want_msg}"
}

@test "referential data causes violation" {
  run bin/gator test \
    -f="$BATS_TEST_DIRNAME/fixtures/policies/default" \
    -f="$BATS_TEST_DIRNAME/fixtures/manifests/referential-data" \
    -o=yaml

  [ "$status" -eq 1 ]

  # Confirm we still get our violation output
  want_msg="ingress host conflicts with an existing ingress <example-host.example.com>" 
  match_yaml_msg "${output[*]}" "${want_msg}"
}

@test "pull OCI images" {
  artifacts_dir="$BATS_TEST_DIRNAME"/../oci-artifacts/
  violating_ns="$BATS_TEST_DIRNAME"/fixtures/manifests/no-policies/violating-ns.yaml
  media_type=:application/vnd.oci.image.layer.v1.tar+gzip

  # Test with 1 OCI image + 1 local directory as input
  img1=localhost:5000/my-bundle:v1

  pushd "$artifacts_dir"
  oras push $img1 ./templates/$media_type ./constraints/$media_type
  popd

  run bin/gator test --image=$img1 --filename="$violating_ns" -o=yaml
  [ "$status" -eq 1 ]
  want_msg="you must provide labels: {\"geo\"}"
  match_yaml_msg "${output[*]}" "${want_msg}"

  # Test with 2 OCI images + stdin as input
  img2=localhost:5000/templates:v1
  img3=localhost:5000/constraints:v1

  pushd "$artifacts_dir"
  run oras push $img2 ./templates/$media_type
  [ "$status" -eq 0 ]
  run oras push $img3 ./constraints/$media_type
  [ "$status" -eq 0 ]
  popd

  run bin/gator test --image=$img2 --image=$img3 -o=yaml < "$violating_ns"
  [ "$status" -eq 1 ]
  want_msg="you must provide labels: {\"geo\"}"
  match_yaml_msg "${output[*]}" "${want_msg}"
}

@test "observe open api v3 defaults being applied" {
  run bin/gator test \
    -f="$BATS_TEST_DIRNAME/fixtures/manifests/with-policies/with-violations-and-defaults.yaml" \
    -o=yaml

  [ "$status" -eq 1 ]

  # these are defined in the template's default fields for the parameters
  want_msg_1="aRequiredLabel" 
  want_msg_2="aRequiredMessage"

  match_substring "${output[*]}" "${want_msg_1}"
  match_substring "${output[*]}" "${want_msg_2}"
}
