#!/usr/bin/env bats

@test "manifest with no violations piped to stdin returns 0 exit status" {
  bin/gator validate < "$BATS_TEST_DIRNAME/fixtures/manifest-no-violations.yaml"
  if [ "$?" -ne 0 ]; then
    printf "ERROR: got exit status %s but wanted 0\n" "$status"
    exit 1
  fi
}

@test "manifest with violations piped to stdin returns 1 exit status" {
  ! bin/gator validate < "$BATS_TEST_DIRNAME/fixtures/manifest-with-violations.yaml"
}

@test 

