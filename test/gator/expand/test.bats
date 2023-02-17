#!/usr/bin/env bats

####################################################################################################
# HELPER FUNCTIONS
####################################################################################################

# match_yaml_in_dir checks that the yaml output (arg1) equals the contents of
# output/output.yaml in the directory (arg2).
match_yaml_in_dir () {
  yaml_output="${1}"
  match_dir="${2}"

  want=$(cat "${BATS_TEST_DIRNAME}"/fixtures/"${match_dir}"/output/output.yaml)
    if [[ ${yaml_output} != *"$want"* ]]; then
      echo "ERROR: resource not found in output"
      echo "WANT: ${want}"
      echo "GOT: ${yaml_output}"
      echo "DIFF: "
      diff <( echo "$want" ) <( echo "$yaml_output" )
      exit 1
    fi
}

# test_dir runs the tests using the directory at fixtures/(arg1)
# The exit code of gator expand is expected to be arg2
test_dir () {
  dir_name="${1}"
 run bin/gator expand --filename="$BATS_TEST_DIRNAME/fixtures/${dir_name}/input" --format=yaml
  # we need `$(expr "#{2}" + 0}` to be an int, so we can't quote it
  # shellcheck disable=SC2046
  [ "$status" -eq $(expr "${2}" + 0) ]
  match_yaml_in_dir "${output}" "${dir_name}"
}

####################################################################################################
# END OF HELPER FUNCTIONS
####################################################################################################

@test "basic generator expands pod with mutators" {
  test_dir "basic-expansion" 0
}

@test "basic generator expands pod with only some configs matching" {
  test_dir "basic-expansion-nonmatching-configs" 0
}

@test "custom resource expands into 2 kinds with mutators" {
 test_dir "expand-cr" 0
}

@test "generator with a custom namespace" {
 test_dir "expand-with-ns" 0
}

@test "generator with a custom namespace but namespace config missing" {
 test_dir "expand-with-missing-ns" 1
}

@test "expand into 2 resultants and write to file" {
  dir_name="expand-cr"
  out_file="tmp.yaml"
 run bin/gator expand --filename="$BATS_TEST_DIRNAME/fixtures/${dir_name}/input" --format=yaml --outputfile="${out_file}"
  [ "$status" -eq 0 ]
  got=$(cat "${out_file}")
  match_yaml_in_dir "${got}" "${dir_name}"
  rm "${out_file}"
}

@test "expand OCI image" {
  fixtures_dir="$BATS_TEST_DIRNAME"/fixtures
  media_type=:application/vnd.oci.image.layer.v1.tar+gzip

  img=localhost:5000/expansion-bundle:v1

  pushd "$fixtures_dir"/basic-expansion
  oras push $img ./input/$media_type
  popd

  run bin/gator expand --image=$img --format=yaml
  [ "$status" -eq 0 ]
  match_yaml_in_dir "${output}" "basic-expansion"
}
