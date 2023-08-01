#!/usr/bin/env bash

#  Copyright 2018 The Kubernetes Authors.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.

set -e

source $(dirname ${BASH_SOURCE})/common.sh

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "${REPO_ROOT}"

header_text "running generate"
make generate

# Only run verify-generate in CI, otherwise running generate
# locally (which is a valid operation) causes `make test` to fail.
if [[ -n ${CI} ]]; then
    header_text "verifying generate"
    make verify-generate
fi

header_text "running modules"
make modules

# Only run verify-modules in CI, otherwise updating
# go module locally (which is a valid operation) causes `make test` to fail.
if [[ -n ${CI} ]]; then
    header_text "verifying modules"
    make verify-modules
fi

header_text "running golangci-lint"
make lint
