#!/bin/bash

. ../../third_party/demo-magic/demo-magic.sh

clear

kubectl apply -f sync.yaml >> /dev/null
kubectl apply -f remediation/k8sbannedimagetags_template.yaml >> /dev/null
sleep 1
kubectl apply -f remediation/ban_latest_tag.yaml >> /dev/null

echo "===== ENTER developer ====="
echo

pe "kubectl create ns advanced-transaction-system"
echo

echo "===== EXIT developer ====="
echo

NO_WAIT=true
p "Five weeks go by. Developer moves on to another project. \"advanced-transaction-system\" is long forgotten until..."
read
p "Our intrepid admin finds it. \"What is advanced-transaction-system? Do we use it?\""
read
p "They go on a three day quest across many departments..."
read
p "Only to find the project was scrapped."
read
p "\"Never again,\" says the admin as they delete the namespace."
NO_WAIT=false

echo
echo "===== ENTER admin ====="
echo

pe "ls -1 templates"
echo

pe "kubectl apply -f templates"
echo

pe "ls -1 constraints"
echo

pe "cat constraints/owner_must_be_provided.yaml"
echo

pe "kubectl apply -f constraints"
echo

echo "===== ENTER developer ======"
echo

pe "kubectl create ns production"
echo

pe "cat good_resources/namespace.yaml"
echo

pe "kubectl apply -f good_resources/namespace.yaml"
echo

pe "kubectl apply -f bad_resources/opa_no_limits.yaml"
echo

pe "kubectl apply -f bad_resources/opa_limits_too_high.yaml"
echo

pe "kubectl apply -f bad_resources/opa_wrong_repo.yaml"
echo

pe "kubectl apply -f good_resources/opa.yaml"
echo

pe "cat bad_resources/duplicate_service.yaml"
echo

pe "kubectl apply -f bad_resources/duplicate_service.yaml"
echo

p "After some more trial and error, the developer's service is up and running"

echo
echo "===== EXIT developer ====="
echo

NO_WAIT=true
p "All is well with the world, until the big outage. The bank is down for hours."
read
p "We must never again use the :latest tag in production!"
echo
NO_WAIT=false

echo "===== ENTER admin ====="
echo

pe "cat remediation/k8sbannedimagetags_template.yaml"
echo

pe "kubectl get k8sbannedimagetags -oyaml"
echo

p "THE END"

kubectl delete -f good_resources
kubectl delete ns advanced-transaction-system
kubectl delete -f constraints
kubectl delete -f templates
kubectl delete -f remediation/ban_latest_tag.yaml
kubectl delete -f remediation/k8sbannedimagetags_template.yaml
