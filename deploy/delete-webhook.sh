#!/bin/bash

cd "${0%/*}"

#set -e
echo "Delete Kubernetes-policy-controller webhook config"

read -p "Press enter to continue"

kubectl delete mutatingwebhookconfiguration kpc