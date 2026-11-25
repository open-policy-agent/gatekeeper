#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "Usage: run-benchmark.sh <checkout-directory> <output-file>" >&2
  exit 2
fi

output=$2
cd "$1"
version="$(awk '
  $1 == "ENVTEST_KUBERNETES_VERSION" {print $3; found = 1; exit}
  $1 == "KUBERNETES_VERSION" {legacy = $3}
  END {if (!found) print legacy}
' Makefile)"
if [[ -z "$version" ]]; then
  echo "::error::No envtest Kubernetes version in $PWD/Makefile (ENVTEST_KUBERNETES_VERSION or KUBERNETES_VERSION)" >&2
  exit 1
fi
echo "Using envtest Kubernetes version $version"

make envtest LOCALBIN="$PWD/.tmp/bin" ENVTEST="$PWD/.tmp/bin/setup-envtest"
assets="$("$PWD/.tmp/bin/setup-envtest" use "$version" --bin-dir "$PWD/.tmp/bin" -p path)"
if [[ -z "$assets" ]]; then
  echo "::error::setup-envtest did not return an assets directory" >&2
  exit 1
fi

KUBEBUILDER_ASSETS="$assets" make benchmark-test BENCHMARK_FILE_NAME="$output"
