#!/bin/bash

set -e

echo "Deploy OPA and kube-mgmt"

read -p "Press enter to continue"

# create opa namespace
kubectl create ns opa

# create tls secret
rm -rf ./secret
mkdir ./secret
cd ./secret

openssl genrsa -out ca.key 2048
openssl req -x509 -new -nodes -key ca.key -days 100000 -out ca.crt -subj "/CN=admission_ca"
cat >server.conf <<EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth, serverAuth
EOF
openssl genrsa -out server.key 2048
openssl req -new -key server.key -out server.csr -subj "/CN=opa.opa.svc" -config server.conf
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -days 100000 -extensions v3_req -extfile server.conf
cd ..

# create k8s opa secret
kubectl -n opa create secret tls opa-server --cert=./secret/server.crt --key=./secret/server.key

# deploy kubernetes-policy-controller 
kubectl apply -n opa -f ./opa.yaml

# deploy kubernetes policies
kubectl -n opa create configmap kubernetes-matches --from-file=../policy/kubernetes/matches.rego

# deploy webhooks
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
