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

set -o errexit
set -o nounset
set -o pipefail

source $(dirname ${BASH_SOURCE})/common.sh

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
cd "${REPO_ROOT}"

APIDIFF="hack/tools/bin/go-apidiff"

header_text "fetching and building go-apidiff"
make "${APIDIFF}"

git status

header_text "verifying api diff"
header_text "invoking: '${APIDIFF} ${PULL_BASE_SHA} --print-compatible'"
"${APIDIFF}" "${PULL_BASE_SHA}" --print-compatible
