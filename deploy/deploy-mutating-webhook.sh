#!/bin/bash

cd "${0%/*}"

set -e
echo "Enable mutating-webhook.kubernetes-policy-controller"

read -p "Press enter to continue"

cat > ./secret/mutating-webhook-configuration.yaml <<EOF
kind: MutatingWebhookConfiguration
apiVersion: admissionregistration.k8s.io/v1beta1
metadata:
  name: mutating.kubernetes-policy-controller
webhooks:
  - name: mutating.webhook.kubernetes-policy-controller
    rules:
      - operations: ["CREATE", "UPDATE"]
        apiGroups: ["*"]
        apiVersions: ["*"]
        resources: ["*"]
    clientConfig:
      caBundle: $(cat ./secret/ca.crt | base64 | tr -d '\n')
      service:
        namespace: opa
        name: opa
        path: "/v1/mutate"
EOF

kubectl -n opa apply -f ./secret/mutating-webhook-configuration.yaml