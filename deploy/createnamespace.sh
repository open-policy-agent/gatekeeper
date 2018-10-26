#!/bin/bash

cd "${0%/*}"

set -e
echo "Create opa namespace"

read -p "Press enter to continue"

# create opa namespace
kubectl create ns opa