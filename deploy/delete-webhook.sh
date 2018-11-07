#!/bin/bash

cd "${0%/*}"

#set -e
echo "Delete Kubernetes-policy-controller webhook config"

read -p "Press enter to continue"

kubectl -n opa delete mutatingwebhookconfiguration mutating.kubernetes-policy-controller