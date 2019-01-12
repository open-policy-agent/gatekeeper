#!/bin/bash

set -e

echo "Deploy OPA and kube-mgmt"

read -p "Press enter to continue"

# create opa namespace
kubectl create ns kpc-system

# deploy kubernetes-policy-controller 
kubectl apply -n kpc-system -f ./deploy/kpc.yaml

# deploy kubernetes policies
kubectl -n kpc-system create configmap kubernetes-matches --from-file=./policy/kubernetes/matches.rego
