#!/bin/bash

cd "${0%/*}"

#set -e
echo "Delete gatekeeper webhook config"

read -p "Press enter to continue"

kubectl delete mutatingwebhookconfiguration gatekeeper