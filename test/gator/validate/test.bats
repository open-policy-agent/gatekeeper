#!/usr/bin/env bats

@test "manifest with no violations piped to stdin returns 0 exit status" {
  run bin/gator validate < "$BATS_TEST_DIRNAME/fixtures/manifest-no-violations.yaml"
  [ "$status" -eq 0 ]
}

@test "manifest with violations piped to stdin returns 1 exit status" {
  run bin/gator validate < "$BATS_TEST_DIRNAME/fixtures/manifest-no-violations.yaml"
  [ "$status" -eq 1 ]
}

