#!/usr/bin/env bash

# Copyright 2016 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Usage: `hack/verify-licenses.sh`.


set -o errexit
set -o nounset
set -o pipefail

KUBE_TEMP=$(mktemp -d 2>/dev/null || mktemp -d -t kubernetes.XXXXXX)


# Creating a new repository tree 
# Deleting vendor directory to make go-licenses fetch license URLs from go-packages source repository
git worktree add -f "${KUBE_TEMP}"/tmp_test_licenses/gatekeeper HEAD >/dev/null 2>&1 || true
cd "${KUBE_TEMP}"/tmp_test_licenses/gatekeeper && rm -rf vendor


# Explicitly opt into go modules, even though we're inside a GOPATH directory
export GO111MODULE=on


allowed_licenses=()
packages_flagged=()
packages_url_missing=()
exit_code=0

# Install go-licenses
echo '[INFO] Installing go-licenses...'
go install github.com/google/go-licenses@latest

# Fetching CNCF Approved List Of Licenses
# Refer: https://github.com/cncf/foundation/blob/main/allowed-third-party-license-policy.md
curl -s 'https://spdx.org/licenses/licenses.json' -o "${KUBE_TEMP}"/licenses.json

number_of_licenses=$(jq '.licenses | length' "${KUBE_TEMP}"/licenses.json)
loop_index_length=$(( number_of_licenses - 1 ))


echo '[INFO] Fetching current list of CNCF approved licenses...'
for index in $(seq 0 $loop_index_length);
do
	licenseID=$(jq ".licenses[$index] .licenseId" "${KUBE_TEMP}"/licenses.json)
	if [[ $(jq ".licenses[$index] .isDeprecatedLicenseId" "${KUBE_TEMP}"/licenses.json) == false ]]
	then
		allowed_licenses+=("${licenseID}")
        fi	
done


# Scanning go-packages under the project & verifying against the CNCF approved list of licenses
echo '[INFO] Starting license scan on go-packages...'
go-licenses report ./... --include_tests >> "${KUBE_TEMP}"/licenses.csv

echo -e 'PACKAGE_NAME  LICENSE_NAME  LICENSE_URL\n' >> "${KUBE_TEMP}"/approved_licenses.dump
while IFS=, read -r GO_PACKAGE LICENSE_URL LICENSE_NAME
do
	FORMATTED_LICENSE_URL=
	if [[ " ${allowed_licenses[*]} " == *"${LICENSE_NAME}"* ]];
	then
		if [[ "${LICENSE_URL}" == 'Unknown' ]];
		then
			if  [[ "${GO_PACKAGE}" != k8s.io/* ]];
			then
				echo "${GO_PACKAGE}  ${LICENSE_NAME}  ${LICENSE_URL}" >> "${KUBE_TEMP}"/approved_licenses_with_missing_urls.dump
				packages_url_missing+=("${GO_PACKAGE}")
			else
				LICENSE_URL='https://github.com/kubernetes/kubernetes/blob/master/LICENSE'
				echo "${GO_PACKAGE}  ${LICENSE_NAME}  ${LICENSE_URL}" >> "${KUBE_TEMP}"/approved_licenses.dump
			fi
		elif curl -Is "${LICENSE_URL}" | head -1 | grep -q 404;
		then
            # For gatekeeper, the script won't find the constraint frameworks's license atm.
            if [[ "${GO_PACKAGE}" == github.com/open-policy-agent/frameworks/* ]];
            then
                LICENSE_URL='https://github.com/open-policy-agent/frameworks/blob/master/LICENSE'
				echo "${GO_PACKAGE}  ${LICENSE_NAME}  ${LICENSE_URL}" >> "${KUBE_TEMP}"/approved_licenses.dump
                continue
            fi

			# Check whether the License URL is incorrectly formed
			# TODO: Remove this workaround check once PR https://github.com/google/go-licenses/pull/110 is merged
			IFS='/' read -r -a split_license_url <<< ${LICENSE_URL}
			for part_of_url in "${split_license_url[@]}"
			do
				if  [[ ${part_of_url} == '' ]]
				then
					continue
				elif	[[ ${part_of_url} == 'https:' ]]
				then
					FORMATTED_LICENSE_URL+='https://'
				else
					if [[ ${part_of_url} =~ ^v[0-9]+\.[0-9]+\.[0-9]+ ]]
					then
						FORMATTED_LICENSE_URL+="${part_of_url}/${split_license_url[-1]}"
						break
					else
						FORMATTED_LICENSE_URL+="${part_of_url}/"
					fi
				fi
			done
			if curl -Is "${FORMATTED_LICENSE_URL}" | head -1 | grep -q 404;
			then
				packages_url_missing+=("${GO_PACKAGE}")
				echo "${GO_PACKAGE}  ${LICENSE_NAME}  ${LICENSE_URL}" >> "${KUBE_TEMP}"/approved_licenses_with_missing_urls.dump
			else
				echo "${GO_PACKAGE}  ${LICENSE_NAME}  ${FORMATTED_LICENSE_URL}" >> "${KUBE_TEMP}"/approved_licenses.dump
			fi
		else
			echo "${GO_PACKAGE}  ${LICENSE_NAME}  ${LICENSE_URL}" >> "${KUBE_TEMP}"/approved_licenses.dump
		fi
	else
        # Not all packages at this point should go to the not approved dump.
        # there are a few exceptions approved by CNCF as per: https://github.com/cncf/foundation/tree/main/license-exceptions
        # Currently gatekeeper uses just one of those so we are not going to do a general solution.

        if [[ "${GO_PACKAGE}" == "github.com/rcrowley/go-metrics" ]] && [[ "${LICENSE_NAME}" == "BSD-2-Clause-FreeBSD" ]];
        then
            # as per https://github.com/cncf/foundation/blob/main/license-exceptions/cncf-exceptions-2019-11-01.json#L723-L726
            echo "${GO_PACKAGE}  ${LICENSE_NAME}  ${LICENSE_URL}" >> "${KUBE_TEMP}"/approved_licenses.dump
        else
		    echo "${GO_PACKAGE}  ${LICENSE_NAME}  ${LICENSE_URL}" >> "${KUBE_TEMP}"/notapproved_licenses.dump
		    packages_flagged+=("${GO_PACKAGE}")
        fi
	fi
done < "${KUBE_TEMP}"/licenses.csv
awk '{ printf "%-100s : %-20s : %s\n", $1, $2, $3 }' "${KUBE_TEMP}"/approved_licenses.dump


if [[ ${#packages_url_missing[@]} -gt 0 ]]; then
	echo -e '\n[ERROR] The following go-packages in the project have unknown or unreachable license URL:'
	awk '{ printf "%-100s :  %-20s : %s\n", $1, $2, $3 }' "${KUBE_TEMP}"/approved_licenses_with_missing_urls.dump
	exit_code=1
fi


if [[ ${#packages_flagged[@]} -gt 0 ]]; then
	echo "[ERROR] The following go-packages in the project are using non-CNCF approved licenses. Please refer to the CNCF's approved licence list for further information: https://github.com/cncf/foundation/blob/main/allowed-third-party-license-policy.md"
	awk '{ printf "%-100s :  %-20s : %s\n", $1, $2, $3 }' "${KUBE_TEMP}"/notapproved_licenses.dump
	exit_code=1
elif [[ "${exit_code}" -eq 1 ]]; then
	echo "[ERROR] Project is using go-packages with unknown or unreachable license URLs. Please refer to the CNCF's approved licence list for further information: https://github.com/cncf/foundation/blob/main/allowed-third-party-license-policy.md"
else
	echo "[SUCCESS] Scan complete! All go-packages under the project are using current CNCF approved licenses!"
fi

exit "${exit_code}"
