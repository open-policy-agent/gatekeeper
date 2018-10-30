#!/bin/bash

cd "${0%/*}"

set -e
echo "Enable validating-webhook.kubernetes-policy-controller"

read -p "Press enter to continue"

cat > ./secret/validating-webhook-configuration.yaml <<EOF
kind: ValidatingWebhookConfiguration
apiVersion: admissionregistration.k8s.io/v1beta1
metadata:
  name: validating.kubernetes-policy-controller
webhooks:
  - name: validating.webhook.kubernetes-policy-controller
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
        path: "/v1/validate"
EOF

kubectl -n opa apply -f ./secret/validating-webhook-configuration.yaml