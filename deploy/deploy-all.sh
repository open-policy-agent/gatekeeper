#!/bin/bash

set -e

echo "Deploy OPA and kube-mgmt"

read -p "Press enter to continue"

# create opa namespace
kubectl create ns gatekeeper-system

# deploy gatekeeper 
kubectl apply -n gatekeeper-system -f ./deploy/gatekeeper.yaml

# deploy kubernetes policies
kubectl -n gatekeeper-system create configmap kubernetes-matches --from-file=./policy/kubernetes/matches.rego
