#!/bin/bash

echo "Delete all will delete all kubebernetes-policy-controller components"

read -p "Press enter to continue"

rm -rf ./secret

kubectl -n opa delete mutatingwebhookconfiguration mutating.kubernetes-policy-controller
kubectl delete -n opa -f ./deploy/opa.yaml
kubectl -n opa delete secret opa-server
kubectl -n opa delete configmap ingress-conflict 
kubectl -n opa delete configmap ingress-host-fqdn 
kubectl -n opa delete configmap annotate
kubectl delete ns opa
