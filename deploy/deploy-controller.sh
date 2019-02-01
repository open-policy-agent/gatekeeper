
#!/bin/bash

cd "${0%/*}"

set -e
echo "Deploy OPA and kube-mgmt"

read -p "Press enter to continue"

# deploy opa 
kubectl apply -n kpc-system -f ./kpc.yaml
