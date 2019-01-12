#!/bin/bash

cd "${0%/*}"

set -e
echo "Create kpc-system namespace"

read -p "Press enter to continue"

# create opa namespace
kubectl create ns kpc-system