
#!/bin/bash

cd "${0%/*}"

echo "Deploy Admission policies"

read -p "Press enter to continue"

# deploy admission policies
kubectl -n opa create configmap ingress-conflict --from-file=../policy/admission/ingress-conflict.rego
kubectl -n opa create configmap ingress-host-fqdn --from-file=../policy/admission/ingress-host-fqdn.rego
kubectl -n opa create configmap annotate --from-file=../policy/admission/annotate.rego