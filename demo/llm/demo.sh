#!/bin/bash

# Pre-requisites:
# Kubernetes cluster that has Gatekepeer with LLM engine installed
# Gator in path
# Kubectl AI (https://github.com/sozercan/kubectl-ai) in path

. ../../third_party/demo-magic/demo-magic.sh

clear

echo "🎬 This is a demo for Gatekeeper with LLM engine."

pe "kubectl get pods -n gatekeeper-system"

echo "🔐 Deploying a policy that requires no privileged containers"

pe "bat privileged-template.yaml"

pe "kubectl apply -f privileged-template.yaml"

pe "bat privileged-constraint.yaml"

pe "kubectl apply -f privileged-constraint.yaml"

pe "kubectl ai 'create a pod that is privileged'"

echo "👉 As expected, this pod got denied because it is privileged"

pe "kubectl ai 'create a pod that is not privileged'"

echo "👉 As expected, this pod did not get denied because it is not privileged"

echo "✅ Deploying a policy that requires images to be from allowed registries"

pe "bat allowedrepos-template.yaml"

pe "kubectl apply -f allowedrepos-template.yaml"

pe "bat allowedrepos-constraint.yaml"

pe "kubectl apply -f allowedrepos-constraint.yaml"

pe "kubectl ai 'create a pod with docker.io/library/nginx:latest image'"

echo "👉 As expected, this pod got denied because it is from a disapproved registry"

pe "kubectl ai 'create a pod with registry.k8s.io/pause:3.8 image'"

echo "👉 As expected, this pod did not get denied because it is not from a disapproved registry"

echo "🐊 Using Gator for shift left validation"

pe "kubectl ai 'create a pod with docker.io/nginx:latest image that is privileged' --raw | gator test --experimental-enable-llm-engine -f ."

echo "👉 As expected, we were able to validate this Kubernetes manifest with Gator for shift left validation"
